package main

import (
	"flag"
	"fmt"
	"github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var (
	name                  string
	managerMode           bool
	agentMode             bool
	agents                string
	port                  int
	cpus                  int
	ignoreConnectionLimit bool
	ignoreHostStr         string
	ignoreHosts           []string

	mHost     string
	mPort     int
	mUser     string
	mPassword string
	mdb       string
	mSocket   string

	rHost         string
	rPort         int
	rPassword     string
	rSocket       string
	timeSensitive bool

	hostProgress = sync.Map{}
	rpool        *redis.Pool
	timeDiff     = 0
)

func parseOptions() {
	flag.BoolVar(&timeSensitive, "ts", true, "time sensitive, understand time diff between captured_time and this command's current time")
	flag.BoolVar(&managerMode, "M", false, "Execute as MQR-applyer-manager")
	flag.BoolVar(&agentMode, "A", false, "Execute as MQR-applyer-agent")
	flag.StringVar(&agents, "agents", "", "specify applyer-agents like (host):(port)")
	flag.IntVar(&port, "p", 6060, "MQR manager/agent use this port")
	flag.StringVar(&name, "name", "", "process name which is used as prefix of redis key")
	flag.BoolVar(&ignoreConnectionLimit, "ignore-limit", false, "Ignore connection limit which is limited to vCPU*3 connections for better performance (not recommended)")

	// mysql
	flag.StringVar(&mHost, "mh", "localhost", "mysql host")
	flag.StringVar(&ignoreHostStr, "ih", "localhost", "ignore mysql hosts, specify only one ip address")
	flag.IntVar(&mPort, "mP", 3306, "mysql port")
	flag.StringVar(&mUser, "mu", "root", "mysql user")
	flag.StringVar(&mPassword, "mp", "", "mysql password")
	flag.StringVar(&mdb, "md", "", "mysql database")
	flag.StringVar(&mSocket, "mS", "", "mysql unix domain socket")

	// redis
	flag.StringVar(&rHost, "rh", "localhost", "redis host")
	flag.IntVar(&rPort, "rP", 6379, "redis port")
	flag.StringVar(&rPassword, "rp", "", "redis password")
	flag.StringVar(&rSocket, "rs", "", "redis unix domain socket file")

	flag.Parse()
}

func main() {
	parseOptions()
	redisHost := rHost + ":" + strconv.Itoa(rPort)
	cpus = runtime.NumCPU()
	rpool = newPool(redisHost)
	ignoreHosts = strings.Split(ignoreHostStr, ",")

	var applyer Applyer

	if managerMode && agentMode {
		fmt.Println("Can not specify both managerMode and agentMode")
		return
	} else if managerMode { // execute as manager (not both manager and agent in one process)
		ags := strings.Split(agents, ",")
		applyer = &managerApplyer{agents: ags}
	} else if agentMode { // as agent (wait http addHost/ endpoint called)
		applyer = &agentApplyer{}
	} else { // single mode (
		applyer = &singleApplyer{}
	}

	err := applyer.prepare()
	if err != nil {
		panic(err)
	}

	applyer.start()
}
