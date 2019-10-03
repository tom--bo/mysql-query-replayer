# MySQL-query-replayer (MQR)

![MQR1](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/multi_agent_mode.png)

MQRはObserver,Queuing MW(Redis), Applyerからなる。
Observerで取得したMySQLのクエリをQueuing MWへ転送し、Applyerがそれを周期的に取得して再現対象のMySQLに実行する。
再現において、MySQLのip:portの組を1コネクションとし、このコネクション数も再現する。
また、コネクション単位でクエリの実行順序、QPSも可能な限り再現することを目標とする。

以降以下の用語を用いて説明する
- Original MySQL: プロダクションで動いているMySQL。このMySQLのネットワークパケットを取得してReplayする
  - Master, Slaveのどちらでも実行可能だが、MQRはCPU, networkに高負荷をかけるので、Active Masterで実行することは推奨しない
- Target MySQL: MQRが取得したクエリを再現する対象のMySQL
- connection(コネクション): Original MySQLに接続しているMySQL clientのip:portを文字列とした1コネクション


## Queuing MW

- redis 1台を使用
- connectionをkey, パケットが到達した時刻のuinix timestampをscoreとしたsorted_setでクエリを保持している
  - 検証用ツールなので, redisの冗長化は考慮していない


## Observer

### 概要

- Original MySQL上で動作させ、libpcapを使ってネットワークパケットを取得、その中からMySQLのクエリを抽出し、Queuing MW(Reids)に送信する。
- デバッグ、可視化用にファイルへの出力、Elasticsearchへの送信が可能
- 1コネクションごとに1スレッド(実際にはgoroutine)を起動する
  - コネクションごとのQPSが高いときは1コネクションで1coreの100%に近いCPUを利用するので注意。
- observerのプログラムを実行した瞬間からパケットを収集し始め、case insensitiveで`SELECT`, `SET`, `SHOW`コマンドを抽出し、モードに従った送信先に転送する。

#### Queuing mode

Original MySQLのネットワークパケットからコマンドを抽出し、Redisに送る通常モード。
-debug, -ehオプションによる`debug mode`, `Elasticsearch mode`で**起動しない**ことでこのモードで動作する
各種オプションの設定はデフォルト値で動作するが、主に以下を設定する必要がある。
- mP (MySQL Port)
- ih (ignore host)
- rh (redis host)
- rP (redis Port)
- rp (redis Password)


#### debug mode

`-debug` オプションを付けることでデバッグモードで動作。
デバッグモードではキャプチャしたクエリをファイルに出力する。
デバッグモードを利用している際に同時に`-c`オプションで数値を指定すると、その数値分のクエリを取得するとコマンドが終了される
`-eh`オプションでelasticsearchへクエリを送信しているときはelasticsearchモードでのデバッグ用出力が行われる。


#### Elasticsearch mode

`-eh`オプションを付けてElasticsearchホストを指定することで、Elasticsearchへ取得したクエリを送信することができる。
Elasticsearchへ送信するのはkibanaでの可視化を目的としているため、ElasticsearchをQueuing MWとして利用することはできない。



### 使い方

ファイルのダウンロードから実行までは以下

1. 実行バイナリをdownloadして実行
1. golangの実行環境を用意して、git clone後、go run observer/main.go (https://github.com/tom--bo/mysql-query-replayer )
1. Redisを用意
1. そのredisをオプションで指定して実行

サンプル
```
./observer -rh 10.127.0.nn -d any -mP 3306 -ih 10.127.0.mm

# golangの実行環境で実行する場合↓
go run main.go -rh 10.127.0.nn -d any -mP 3306 -ih 10.127.0.mm
```

終了時は、`-debug`, `-c <count>`を両方指定している場合を除き、自動で停止しないので、`ctrl-c`や`kill`コマンドを利用して停止。



### options

| オプション | 説明 |
|:---:|:---|
| debug | デバッグモードにするオプション、-ehと同時に指定しない場合、ファイルにクエリを出力する。-ehと同時に指定された場合はElasticsearchモードのdebug出力を行う |
| c <num> | count, デバッグモードと同時に指定されると<num>分のパケットを取得した後にコマンドを終了する |
| f <filename> | <filename>で指定したtcpdumpのダンプファイルからクエリを取得し動作する。あとからの検証用にパケットを保存しておく場合に有効 |
| d <device> | NICのデバイスインタフェースを指定する。すべての場合は`any`。不要なNICのパケットをフィルタリングすることで、パケットのロストを防ぐことができる。 |
| s <num> | snapshot length, tcpdumpにおけるsnapshotLengthと同じ。これで指定したbyte数分だけでのパケットをやめる。先頭1024byte分のパケットのbyte文字列を取得するなど。不要に長いパケットを刻むことでパケットのロストを防ぐことができる。 |
| pr | promiscuousモードで動作。tcpdumpのpromiscuousオプションと同じ。 |
| mh <host> | MySQL Host, Original MySQLのホストを指定。 |
| mP <port> | MySQL Port, Original MySQLのポートを指定 |
| ih <host:port> | Ignore Host, クエリ取得から除外するclient hostを指定。mmmのエージェントとかを無視しないとコネクションが大量になる。 |
| rh <host> | Redis Host, Redisのホストを指定 |
| rP <port> | Redis Port, Redisのportを指定 |
| rp <password> | Redis Password, Redisのpasswordを指定 |
| eh <host> | Elasticsearch Host |
| eP <port> | Elasticsearch port |
| ep <password> | !無い! Elasticsearch Password |
|  |  |


## Applyer

### 概要

Queuing MW(現状ではRedisのみを想定)からObserverが抽出したコマンドを短時間のpollingで取得し、Target MySQLに実行(再現)する。
1コマンドで実行できるSingle MODEと複数台のサーバを用意してApplyerの負荷分散を行うためのManager/Agent MODEがある。
Manager/Agent ModeはOriginal MySQLへのクライアント(Application server)が複数台、複数コネクションを張っている状況のクエリを再現する際、Applyer1台でQPSを再現することは不可能な環境で利用することを想定している。

1プロセス(1コマンドでの実行)でSingle MODE, Manager MODE, Agent MODEを兼任することはできない。
Single MODE, Agent MODEではgoroutineによって、並列にクエリの再現を行っており、mainのgoroutineに加え(connection * 2)のgoroutineを立てており、再現できるコネクションの数は`(CPU core数 / 2) - 1` である。


### 使い方 (Single MODE)

![MQR single mode 画像](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/single_agent_mode.png)


`Single MODE`ではApplyerを実行するコマンドでApplyの処理が完結するため、Observerと紐付いているRedisとTarget MySQLの準備以外で必要な準備はない。

Redis, Target MySQLのACLを確認し、適切なhost(ip:port), user, password等をオプションで指定する。
ファイルのダウンロードから実行までは以下


1. 実行バイナリをdownloadして実行
1. golangの実行環境を用意して、git clone後、go run observer/main.go (https://github.com/tom--bo/mysql-query-replayer )

コマンドサンプル
```
./applyer -mh 10.127.1.nn -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.mm -mp mqr_passwd

# または
go run main.go -mh 10.127.1.nn -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.mm -mp mqr_passwd
```


### 使い方 (複数台) (Manager/Agent MODE)

![MQR multi mode 画像](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/multi_agent_mode.png)

Manager/Agent MODEはOriginal MySQLに接続しているclientが多数の場合に、Applyerを複数台にすることで本番と同等のQPSを再現することを目的としている。
Manager MODEで起動するプロセスが1つ、Agent MODEで起動するホストがN台で動作することが可能。

Redis, Target MySQLが動作していることを前提にし、起動する順序は以下

1. Agent MODEのプロセスすべてを起動
1. Manager MODEのプロセス起動

リアルタイムにOriginal MySQLのコマンドを抽出する場合はManager MODEのプロセスを起動した後にObserverを起動する。

ファイルのダウンロードから実行までは以下

1. 実行バイナリをdownloadして実行
1. golangの実行環境を用意して、git clone後、go run observer/main.go (https://github.com/tom--bo/mysql-query-replayer )
1. Agent MODEのapplyerを起動 (-Aオプションを指定)
1. Manager MODEのapplyerを起動 (-Mオプションを指定)

![MQR multi mode 画像](https://github.com/tom--bo/mysql-query-replayer/blob/master/images/multi_agent_mode_with_steps.png)

Agent MODEのapplyer起動, コマンドサンプル
```
./applyer -A -mh 10.127.1.nn -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.mm -mp mqr_passwd

# または
go run main.go -A -mh 10.127.1.nn -mP 3306 -mu mqr_user -md mqr_db -rh 10.127.1.mm -mp mqr_passwd
```

Manager MODEのapplyer起動, コマンドサンプル
```
./applyer -M -agents 10.127.149.16:6060,10.127.156.69:6060,10.127.56.106:6060 -rh 10.127.159.147

# または
go run main.go -M -agents 10.127.149.16:6060,10.127.156.69:6060,10.127.56.106:6060 -rh 10.127.159.147
```


### options

| オプション | 説明 |
|:---:|:---|
| ts | time sensitive, queueにあるコマンドを即時実行するのではなく、コマンドが実行された時間との差分を考慮して、同じ間隔でコマンドを実行する。 特にobserver側でpcapのダンプファイルからコマンドを取得した場合に有効 |
| M | Manager MODEで起動する。 同時に-agentオプションを指定する必要があり、これで指定されたエージェントに対してコネクションを指定すマネージャとして起動する。このモードではqueueからの取得や取得したコマンドのTarget MySQLへの適用は行わない。 |
| A | Agent MODEで起動する。Manager MODEのプロセスより先に起動する必要がある。Agentとしてデフォルト6060　portでManagerからの支持を待機し、受け取ったコネクションのコマンドを適用する。 |
| agents | Manager MODEで起動する際にAgent MODEで起動しているホストを指定するオプション。コマンド区切りでhostIP:portを複数指定可能。Manager MODEで起動するプロセスで指定が必須。 |
| p | Agent MODEでManegerからの支持を待機するポートを指定する。デフォルト6060。Agent MODEを変更した場合はManager MODEでもportをあわせる必要がある。 |
| mh <host> | MySQL Host, Target MySQLのホストを指定。 |
| mP <port> | MySQL Port, Target MySQLのポートを指定 |
| mu <user> | MySQL user, Target MySQLのuserを指定 |
| mp <password> | MySQL Password, Target MySQLのpasswordを指定 |
| md <database> | MySQL Database, Target MySQLのdatabaseを指定, 1つしか指定できない |
| rh <host> | Redis Host, Redisのホストを指定 |
| rP <port> | Redis Port, Redisのportを指定 |
| rp <password> | Redis Password, Redisのpasswordを指定 |
|  |  |



## mpReader

dumpファイルを直接読み込んでTarget MySQLへ実行する。

### 使い方

```
./mpReader -f dump.pcap -h 10.233.76.xxx -P 3306 -u tombo -d sysbench -p password
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

