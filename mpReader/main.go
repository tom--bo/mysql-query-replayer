package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	mp "github.com/tom--bo/mysql-packet-deserializer"
	"io"
	"log"
	"strconv"
	"strings"
	"time"
)

var (
	debug      bool
	err        error
	read_only  bool   = true
	timelayout string = "2006-01-02 15:04:05.000000"

	handle      *pcap.Handle
	packetCount int
	pcapfile    string

	db            *sql.DB
	mHost         string
	mPort         int
	mUser         string
	mPassword     string
	ignoreHostStr string
	ignoreHosts   []string

	mdb string
)

type MySQLPacketInfo struct {
	srcIP        string
	srcPort      int
	dstIP        string
	dstPort      int
	mysqlPacket  []mp.IMySQLPacket
	capturedTime time.Time
}

func parseOptions() {
	flag.BoolVar(&debug, "debug", false, "debug")
	flag.StringVar(&pcapfile, "f", "", "pcap file. this option invalid packet capture from devices.")
	flag.IntVar(&packetCount, "c", 0, "Limit processing packets count (only enable when -debug is also specified)")

	// MySQL
	flag.StringVar(&mHost, "h", "localhost", "mysql host")
	flag.IntVar(&mPort, "P", 3306, "mysql port")
	flag.StringVar(&mUser, "u", "root", "mysql user")
	flag.StringVar(&mPassword, "p", "", "mysql password")
	flag.StringVar(&mdb, "d", "", "mysql database")
	flag.StringVar(&ignoreHostStr, "ih", "localhost", "ignore mysql hosts, specify only one ip address")

	flag.Parse()
}

func checkRequiredOptions() error {
	var err error
	err = nil
	if pcapfile == "" {
		fmt.Println("dumpfile is not specified!! Use -f to specify")
		err = errors.New("Required option is not specified")
	}

	return err
}

func getMySQLPacketInfo(packet gopacket.Packet) (MySQLPacketInfo, error) {
	applicationLayer := packet.ApplicationLayer()
	if applicationLayer == nil {
		return MySQLPacketInfo{}, errors.New("invalid packets")
	}

	frame := packet.Metadata()
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if ipLayer == nil || tcpLayer == nil {
		return MySQLPacketInfo{}, errors.New("Invalid_Packet")
	}

	ip, _ := ipLayer.(*layers.IPv4)
	tcp, _ := tcpLayer.(*layers.TCP)
	mcmd := mp.DeserializePacket(applicationLayer.Payload())
	if len(mcmd) == 0 {
		return MySQLPacketInfo{}, errors.New("Not_MySQL_Packet")
	}
	return MySQLPacketInfo{ip.SrcIP.String(), int(tcp.SrcPort), ip.DstIP.String(), int(tcp.DstPort), mcmd, frame.CaptureInfo.Timestamp}, nil
	// return MySQLPacketInfo{"srcIP", int(tcp.SrcPort), "dstIP", int(tcp.DstPort), mcmd, frame.CaptureInfo.Timestamp}, nil
}

func checkReadQuery(q string) bool {
	q = strings.TrimSpace(q)
	if strings.HasPrefix(q, "select") || strings.HasPrefix(q, "SELECT") {
		return true
	}
	return false
}

func execQuery(packet gopacket.Packet) error {
	applicationLayer := packet.ApplicationLayer()
	if applicationLayer != nil {
		pInfo, err := getMySQLPacketInfo(packet)
		if err != nil {
			fmt.Println(err)
			return err
		}
		if isIgnoreHosts(pInfo.srcIP, ignoreHosts) {
			// fmt.Println(pInfo.srcIP + " is specified as ignoring host")
			return nil
		}

		if pInfo.mysqlPacket[0].GetCommandType() == mp.COM_QUERY {
			cmd := pInfo.mysqlPacket[0].(mp.ComQuery)
			q := makeOneLine(cmd.Query)
			if read_only && !checkReadQuery(q) {
				return errors.New("not read query")
			}
			_, err := db.Exec(q)
			if err != nil {
				fmt.Println(err)
				return err
			}
			return nil
		} else {
			return errors.New("not COM_QUERY")
		}
	}
	return errors.New("something is wrong")
}

func makeOneLine(q string) string {
	q = strings.Replace(q, "\"", "'", -1)
	q = strings.Replace(q, "\n", " ", -1)

	return q
}

func printQuery(packet gopacket.Packet) error {
	applicationLayer := packet.ApplicationLayer()
	if applicationLayer != nil {
		pInfo, err := getMySQLPacketInfo(packet)
		if err != nil {
			fmt.Println(err)
			return err
		}
		if isIgnoreHosts(pInfo.srcIP, ignoreHosts) {
			// fmt.Println(pInfo.srcIP + " is specified as ignoring host")
			return nil
		}
		if pInfo.mysqlPacket[0].GetCommandType() == mp.COM_QUERY {
			cmd := pInfo.mysqlPacket[0].(mp.ComQuery)
			q := makeOneLine(cmd.Query)
			if read_only && !checkReadQuery(q) {
				return errors.New("not read query")
			}
			fmt.Printf("%26s, %s \n", pInfo.capturedTime.Format(timelayout), q)
			if err != nil {
				fmt.Println(err)
				return err
			}
			return nil
		} else {
			return errors.New("not COM_QUERY")
		}
	}
	return errors.New("something is wrong")
}

func isIgnoreHosts(ip string, ignoreHosts []string) bool {
	for _, h := range ignoreHosts {
		if ip == h {
			return true
		}
	}
	return false
}

func main() {
	parseOptions()
	ignoreHosts = strings.Split(ignoreHostStr, ",")
	err = checkRequiredOptions()
	if err != nil {
		return
	}

	mysqlHost := mUser + ":" + mPassword + "@tcp(" + mHost + ":" + strconv.Itoa(mPort) + ")/" + mdb + "?loc=Local&parseTime=true"
	db, err = sql.Open("mysql", mysqlHost)
	if err != nil {
		fmt.Println("Connection to MySQL fail.")
		fmt.Println(err)
	}
	defer db.Close()

	handle, err = pcap.OpenOffline(pcapfile)
	if err != nil {
		log.Fatal(err)
		panic(err)
	}
	defer handle.Close()

	filter := "tcp and tcp[13] & 8 != 0" // tcp PSH flag is set (more smart filtering needed!)
	if mPort != 0 {
		filter += " and port " + strconv.Itoa(mPort)
	}

	err = handle.SetBPFFilter(filter)
	if err != nil {
		log.Fatal(err)
	}

	// Use the handle as a packet source to process all packets
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	cnt := 0
	for {
		if packetCount != 0 && cnt >= packetCount {
			break
		}
		packet, err := packetSource.NextPacket()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Println(err)
			continue
		}

		if debug {
			err = printQuery(packet)
		} else {
			err = execQuery(packet)

		}
		if err == nil {
			cnt += 1
		}
	}
}
