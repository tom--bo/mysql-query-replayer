package main

import (
	"database/sql"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"github.com/labstack/echo"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type agentApplyer struct {
	hostCnt  int
	cpuLimit int
}

func (a *agentApplyer) prepare() error {
	cpus = runtime.NumCPU()
	a.cpuLimit = cpus * 3
	a.hostCnt = 0
	return nil
}

func (a *agentApplyer) start() {
	// Start server and set addHostHandler, OkHandler
	a.agentServer()
}

func (a *agentApplyer) retrieveLoop(key string, m *sync.Mutex, q *[]commandData) {
	for {
		m.Lock()
		ll := len(*q)
		m.Unlock()
		if ll > 10000 {
			// fmt.Println("more than 1000")
			time.Sleep(50 * time.Millisecond)
			continue
		}

		r := rpool.Get()
		queries, err := redis.Strings(r.Do("LRANGE", key, 0, 199))
		r.Close()
		if err != nil {
			fmt.Println(err)
		}
		l := len(queries)
		if l < 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		r = rpool.Get()
		_, err = r.Do("LTRIM", key, l, -1)
		if err != nil {
			fmt.Println(err)
		}
		r.Close()

		tmp := []commandData{}
		for i := 0; i < l; i++ {
			// ?? need judgement of command_type
			val := strings.SplitN(queries[i], ";", 3)
			capturedTime, err := strconv.Atoi(val[1])
			if err != nil {
				panic(err)
			}
			st := commandData{
				ctype:        val[0],
				capturedTime: capturedTime,
				query:        val[2],
			}
			tmp = append(tmp, st)
		}
		m.Lock()
		*q = append(*q, tmp...)
		m.Unlock()
	}
}

func (a *agentApplyer) applyLoop(mq *sync.Mutex, q *[]commandData) {
	mysqlHost := mUser + ":" + mPassword + "@tcp(" + mHost + ":" + strconv.Itoa(mPort) + ")/" + mdb + "?loc=Local&parseTime=true"
	if mSocket != "" {
		mysqlHost = mUser + ":" + mPassword + "@unix(" + mSocket + ")/" + mdb + "?loc=Local&parseTime=true"
	}

	db, err := sql.Open("mysql", mysqlHost)
	if err != nil {
		fmt.Println("Connection to MySQL fail.")
	}
	defer db.Close()
	var l, ll int

	for {
		mq.Lock()
		ll = len(*q)
		mq.Unlock()
		if ll == 0 {
			continue
		}
		mq.Lock()
		queries := make([]commandData, ll)
		l = copy(queries, *q)
		*q = []commandData{}
		mq.Unlock()
		if timeSensitive && timeDiff == 0 {
			n := time.Now()
			if err != nil {
				panic(err)
			}
			timeDiff = int(n.UnixNano()) - queries[0].capturedTime*1000
		}
		for i := 0; i < l; i++ {
			// send query to mysql
			if err != nil {
				panic(err)
			}

			now := int(time.Now().UnixNano())
			sleepTime := queries[i].capturedTime*1000 + timeDiff - now
			if timeSensitive && sleepTime > 0 {
				time.Sleep(time.Duration(sleepTime) * time.Nanosecond)
				i -= 1
				continue
			}

			if queries[i].ctype == "Q" { // simple query
				_, err := db.Exec(queries[i].query)
				if err != nil {
					fmt.Println(err)
					continue
				}
			} else if queries[i].ctype == "P" {
				// Prepare prepared_statement
			} else if queries[i].ctype == "E" {
				// Execute prepared_statement
			}
		}
	}
}

func (a *agentApplyer) okHandler(c echo.Context) error {
	return c.String(http.StatusOK, "OK!")
}

func (a *agentApplyer) addHostHandler(c echo.Context) error {
	k := c.Param("key")

	ips := strings.Split(k, ":")
	if isIgnoreHosts(ips[1], ignoreHosts) {
		fmt.Println(ips[1] + " is specified as ignoring host")
		return c.String(http.StatusOK, "No")
	}
	if !ignoreConnectionLimit {
		if a.hostCnt >= a.cpuLimit {
			fmt.Println("Too many hosts, ignore " + k)
			return c.String(http.StatusOK, "No")
		}
	}

	a.hostCnt += 1
	q := []commandData{}
	m := new(sync.Mutex)

	hostProgress.Store(k, "0")
	go a.retrieveLoop(k, m, &q)
	go a.applyLoop(m, &q)

	return c.String(http.StatusOK, "Added!")
}

func (a *agentApplyer) agentServer() {
	e := echo.New()
	e.GET("/ok", a.okHandler)
	e.POST("/addHost/:key", a.addHostHandler)
	addr := ":" + strconv.Itoa(port)
	e.Logger.Fatal(e.Start(addr))
}
