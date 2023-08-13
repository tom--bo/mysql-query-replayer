package main

import (
	"github.com/garyburd/redigo/redis"
	"strings"
	"time"
)

type commandData struct {
	ctype        string
	capturedTime int
	query        string
	val1         string
	val2         string
}

type Applyer interface {
	prepare() error
	start()
}

func checkKeys(prefix string) ([]string, error) {
	c := rpool.Get()
	defer c.Close()

	keys, err := redis.Strings(c.Do("keys", "*"))
	if err != nil {
		return []string{}, err
	}

	ret := []string{}
	for _, v := range keys {
		if prefix == "" {
			if strings.HasPrefix(v, ":") {
				ret = append(ret, v)
			}
		} else if strings.HasPrefix(v, prefix) {
			ret = append(ret, v)
		}
	}

	return ret, nil
}

func newPool(addr string) *redis.Pool {
	f := func() (redis.Conn, error) { return redis.Dial("tcp", addr) }

	if rSocket != "" && rPassword != "" {
		f = func() (redis.Conn, error) { return redis.Dial("unix", rSocket, redis.DialPassword(rPassword)) }
	} else if rSocket != "" {
		f = func() (redis.Conn, error) { return redis.Dial("unix", rSocket) }
	} else if rPassword != "" {
		f = func() (redis.Conn, error) { return redis.Dial("tcp", addr, redis.DialPassword(rPassword)) }
	}

	return &redis.Pool{
		MaxIdle:     cpus*10 + 1,
		MaxActive:   cpus*10 + 1,
		IdleTimeout: 2 * time.Second,
		Dial:        f,
	}
}

func isIgnoreHosts(ip string, ignoreHosts []string) bool {
	for _, h := range ignoreHosts {
		if ip == h {
			return true
		}
	}
	return false
}
