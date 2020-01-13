# MySQL-query-replayer (MQR)

[Japanese version README(日本語)](README_ja.md)

![MQR1](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/multi_agent_mode.png)

MySQL Query Replayer(MQR) is the tool to reproduce clients queries by capturing TCP packets.
MQR-observer extract queries and send them to Queueing MW(support only Redis now), and MQR-Applyer apply them to Target MySQL.
This can extract not only network packets in real time by using libpcap but also reading `tcpdump` output files.

Main goal of MQR is duplicate production queries to Target MySQL with the same QPS, same connections as the production environment. And with same order as much as possible.

## Queuing MW

- Support only Redis `sorted-set`
  - key: `${ip}:${port}`
  - score: timestamp in the packets
  - value: Query


## Observer

MQR-observer extract queries and send them to Queueing MW(support only Redis now) in real time.
This affects the performance of MySQL server, so It's not recommended to use this on busy server.
Instead of using this in real time, you can use `tcpdump` and use the output later.

#### Queuing mode

Basic mode, It extracts network packets and send them to redis server.

- mP (MySQL Port)
- ih (ignore host)
- rh (redis host)
- rP (redis Port)
- rp (redis Password)


#### Debug mode

With `-debug` option, MQR-observer prints the extracted queries.
You can also specify `-c {count}` option to limit packets count, and stop after extracting specified queries.


#### Elasticsearch mode

With `-eh {elasticsearch-host}` option, MQR-observer send extracted queries to Elasticsearch and analyze and visualize with `Kibana` easily.


### How to use

```
git clone https://github.com/tom--bo/mysql-query-replayer
cd mysql-query-replayer/observer
go build .

# samples
./observer -rh 10.127.0.10 -d any -mP 3306 -ih 10.127.0.20
```


### options

| options | description |
|:---:|:---|
| debug | Running in debug mode, print extracted queries |
| c <num> | Limit extracting queries and stop |
| f <filename> | Extract from <filename> |
| d <device> | NIC device(default `any`) used by libpcap |
| s <num> | snapshot length used by libpcap snapshotLength |
| pr | promiscuous mode used by libpcap |
| mh <host> | Original MySQL host |
| mP <port> | Original MySQL port |
| ih <host:port> | Ignoring Host |
| rh <host> | Redis Host |
| rP <port> | Redis Port |
| rp <password> | Redis Password |
| eh <host> | Elasticsearch Host |
| eP <port> | Elasticsearch port |


## Applyer


MQR-applyer poll the Queueing MW and apply them to Target MySQL.
Applyer has SINGLE mode and MANAGER/AGENT mode to apply queries with same amount of connections and QPS.


### Single MODE

![MQR single mode 画像](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/single_agent_mode.png)

It's easy to run MQR in single mode.
Let's specify Redis server and Target MySQL, that's all.

```
# sample
./applyer -mh 10.127.1.10 -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.20 -mp mqr_passwd
```


### Manager/Agent MODE

![MQR multi mode 画像](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/multi_agent_mode.png)

Manager/Agent MODE aims to reproduce the same QPS as the actual queries with same amount of connections and QPS, using multiple Applyers.
One process that starts in Manager MODE, and N hosts that start in Agent MODE can realize this .

Assuming that MQR-observer, Redis(queueing MW) and Target MySQL is running, the startup order is as follows

1. Start the Agent MODE processes
1. Start the Manager MODE processes

(Agent MODE)
```
./applyer -A -mh 10.127.1.10 -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.20 -mp mqr_passwd
```

(Manager MODE)
```
./applyer -M -agents 10.127.149.16:6060,10.127.156.69:6060,10.127.56.106:6060 -rh 10.127.159.147
```

## mpReader

This command do not use queueing MW, just read tcpdump output and replay queries.
Extract queries from `tcpdump` output file and replay to Target MySQL.

### How to use

```
./mpReader -f dump.pcap -h 10.233.76.10 -P 3306 -u tombo -d sysbench -p password
```

### options

```
  -P int
    	mysql port (default 3306)
  -c int
    	Limit processing packets count (only enable when -debug is also specified)
  -d string
    	mysql database
  -debug
    	debug
  -f string
    	pcap file. this option invalid packet capture from devices.
  -h string
    	mysql host (default "localhost")
  -ih string
    	ignore mysql hosts, specify only one ip address (default "localhost")
  -p string
    	mysql password
  -u string
    	mysql user (default "root")
```




## Suport environments

- Centos >= 6.9
- MySQL >= 5.5 (>= 5.1 as well as possible)
	- Theoretically, MySQL client/server protocol has compatibility from MySQL-v4.1
- (golang >= 1.11) if you will develop MQR, you need not consider about go version because you can use built binary files.



## How to get packets with tcpdump

Please specify the `dst port 3306` if it's possible.

`tcpdump -n -nn -s 0 -i some-itfc dst port 3306 -B 4096 -c 10000 -w filename.pcap`

The return packets from MySQL-server contains arbitrary data which happens to match the client packet header.
If it happens to match, the parser will work unintendedly.
