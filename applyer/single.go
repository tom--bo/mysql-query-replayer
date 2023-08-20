package main

import (
	"database/sql"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type singleApplyer struct {
	cpuLimit     int
	m            sync.Mutex
	q            []commandData
	hostProgress sync.Map
}

func (a *singleApplyer) prepare() error {
	cpus = runtime.NumCPU()
	a.cpuLimit = cpus * 3
	a.q = []commandData{}
	a.m = sync.Mutex{}
	return nil
}

func (a *singleApplyer) start() {
	keyMap := make(map[string]int)
	hostCnt := 0

	go a.retrieveLoop()
	go a.applyLoop()

	for {
		keys, err := checkKeys(name)
		if err != nil {
			fmt.Printf("%v\n", err)
		}
		for _, k := range keys {
			if _, ok := keyMap[k]; !ok {
				keyMap[k] = 0
				ips := strings.Split(k, ":")
				if isIgnoreHosts(ips[1], ignoreHosts) {
					fmt.Println(ips[1] + " is specified as ignoring host")
					continue
				}
				if !ignoreConnectionLimit {
					if hostCnt <= a.cpuLimit {
						a.hostProgress.Store(k, "0")
						hostCnt += 1
					} else {
						fmt.Println("Too many hosts, ignore " + k)
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (a *singleApplyer) retrieveLoop() {
	pMap := map[string]string{}

	for {
		a.hostProgress.Range(func(k, v interface{}) bool {
			pMap[k.(string)] = v.(string)
			return true
		})
		for k := range pMap {
			for { // wait until queue(a.q) length is less than 10000
				a.m.Lock()
				ll := len(a.q)
				a.m.Unlock()
				if ll > 10000 {
					// fmt.Println("more than 1000")
					time.Sleep(50 * time.Millisecond)
					continue
				}
				break
			}

			r := rpool.Get()
			queries, err := redis.Strings(r.Do("LRANGE", k, 0, 199))
			r.Close()
			if err != nil {
				fmt.Println(err)
			}
			l := len(queries)
			if l < 1 {
				continue
			}
			r = rpool.Get()
			_, err = r.Do("LTRIM", k, l, -1)
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
					fmt.Println(err)
					continue
				}
				st := commandData{
					ctype:        val[0],
					capturedTime: capturedTime,
					query:        val[2],
				}
				tmp = append(tmp, st)
			}
			a.m.Lock()
			a.q = append(a.q, tmp...)
			a.m.Unlock()
		}
	}
}

func (a *singleApplyer) applyLoop() {
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
		a.m.Lock()
		ll = len(a.q)
		a.m.Unlock()
		if ll == 0 {
			continue
		}
		a.m.Lock()
		queries := make([]commandData, ll)
		l = copy(queries, a.q)
		a.q = []commandData{}
		a.m.Unlock()
		if timeSensitive && timeDiff == 0 {
			n := time.Now()
			if err != nil {
				panic(err)
			}
			timeDiff = int(n.UnixNano()) - queries[0].capturedTime*1000
		}
		// fmt.Println("applyLoop m.Unlock()")
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
