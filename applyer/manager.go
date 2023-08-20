package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type managerApplyer struct {
	agents             []string
	tooManyConnections bool
}

func (a *managerApplyer) prepare() error {
	a.tooManyConnections = false

	err := a.checkMQRAgents()
	if err != nil {
		return err
	}
	return nil
}

func (a *managerApplyer) start() {
	keyMap := make(map[string]int)
	connCnt := 0
	agentCnt := len(a.agents)
	for {
		keys, err := checkKeys(name)
		if err != nil {
			fmt.Printf("%v\n", err)
		}
		for _, k := range keys {
			if _, ok := keyMap[k]; !ok {
				fmt.Println("New connection (" + k + ") is detected, " + strconv.Itoa(connCnt) + " is applied")
				ips := strings.Split(k, ":")
				keyMap[k] = connCnt
				if isIgnoreHosts(ips[1], ignoreHosts) {
					fmt.Println(ips[1] + " is specified as ignoring host")
					continue
				}
				go func() {
					err = a.addHostRequest(connCnt, k)
					if err != nil {
						fmt.Printf("%v\n", err)
					}
				}()
				connCnt = (connCnt + 1) % agentCnt
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *managerApplyer) addHostRequest(num int, key string) error {
	if a.tooManyConnections {
		fmt.Println(a.agents[num] + " fail")
		return nil
	}
	agentCnt := len(agents)
	for i := 0; i < agentCnt; i++ {
		n := (num + i) % agentCnt
		url := "http://" + a.agents[n] + "/addHost/" + key
		resp, err := http.Post(url, "", nil)
		if err != nil {
			panic(err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		if err != nil {
			fmt.Println(err)
			fmt.Println(a.agents[n] + " fail")
			resp.Body.Close()
			continue
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fmt.Println(resp.StatusCode)
			fmt.Println(a.agents[n] + " fail")
			resp.Body.Close()
			continue
		} else if string(body) == "no" {
			fmt.Println(a.agents[n] + " is full.")
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		// success
		return nil
	}

	a.tooManyConnections = true
	return nil
}

func (a *managerApplyer) checkMQRAgents() error {
	if a.agents[0] == "" {
		return errors.New("Failed to execute as MQR-applyer-manager: No MQR-agents are specified, use -agents option to specify MQR-agents")
	}
	for _, agent := range a.agents {
		url := "http://" + agent + "/ok"
		resp, err := http.Get(url)
		if err != nil {
			fmt.Println(err)
			fmt.Println(agent + "'s /ok didn't respond")
			return err
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fmt.Println(resp.StatusCode)
			return errors.New(agent + "'s /ok didn't respond")
		}
		resp.Body.Close()
	}
	return nil
}
