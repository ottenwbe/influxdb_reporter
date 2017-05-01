/*
Copyright (c) 2017 Beate Ottenwälder

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/cloudfoundry/gosigar"
	influx "github.com/influxdata/influxdb/client/v2"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type collectionResult struct {
	point []*influx.Point
	err   error
}

type collectionFunc func(chan collectionResult)

// Variables storing arguments flags
const applicationVersion = "0.6.0-alpha"

var verboseFlag bool
var versionFlag bool
var daemonFlag bool
var daemonIntervalFlag time.Duration
var daemonConsistencyFlag time.Duration
var consistencyFactor = 1.0
var collectFlag string

var pidFile string
var sslFlag bool
var hostFlag string
var usernameFlag string
var passwordFlag string
var secretFlag string
var databaseFlag string

var retentionPolicyFlag string

func init() {
	flag.BoolVar(&versionFlag, "version", false, "Print the version number and exit.")
	flag.BoolVar(&versionFlag, "V", false, "Print the version number and exit (shorthand).")

	flag.BoolVar(&verboseFlag, "verbose", false, "Display debug information: choose between text or JSON.")
	flag.BoolVar(&verboseFlag, "v", false, "Display debug information: choose between text or JSON (shorthand).")

	flag.BoolVar(&sslFlag, "ssl", false, "Enable SSL/TLS encryption.")
	flag.BoolVar(&sslFlag, "S", false, "Enable SSL/TLS encryption (shorthand).")
	flag.StringVar(&hostFlag, "host", "localhost:8086", "Connect to host.")
	flag.StringVar(&hostFlag, "h", "localhost:8086", "Connect to host (shorthand).")
	flag.StringVar(&usernameFlag, "username", "root", "User for login.")
	flag.StringVar(&usernameFlag, "u", "root", "User for login (shorthand).")
	flag.StringVar(&passwordFlag, "password", "root", "Password to use when connecting to server.")
	flag.StringVar(&passwordFlag, "p", "root", "Password to use when connecting to server (shorthand).")
	flag.StringVar(&secretFlag, "secret", "", "Absolute path to password file (shorthand). '-p' is ignored if specifed.")
	flag.StringVar(&secretFlag, "s", "", "Absolute path to password file. '-p' is ignored if specifed.")
	flag.StringVar(&databaseFlag, "database", "", "Name of the database to use.")
	flag.StringVar(&databaseFlag, "d", "", "Name of the database to use (shorthand).")
	flag.StringVar(&retentionPolicyFlag, "retentionpolicy", "", "Name of the retention policy to use.")
	flag.StringVar(&retentionPolicyFlag, "rp", "", "Name of the retention policy to use (shorthand).")

	flag.StringVar(&collectFlag, "collect", "cpu,cpus,mem,swap,uptime,load,network,disks,mounts", "Chose which data to collect.")
	flag.StringVar(&collectFlag, "c", "cpu,cpus,mem,swap,uptime,load,network,disks,mounts", "Chose which data to collect (shorthand).")

	flag.BoolVar(&daemonFlag, "daemon", false, "Run in daemon mode.")
	flag.BoolVar(&daemonFlag, "D", false, "Run in daemon mode (shorthand).")
	flag.DurationVar(&daemonIntervalFlag, "interval", time.Second, "With daemon mode, change time between checks.")
	flag.DurationVar(&daemonIntervalFlag, "i", time.Second, "With daemon mode, change time between checks (shorthand).")
	flag.DurationVar(&daemonConsistencyFlag, "consistency", time.Second, "With custom interval, duration to bring back collected values for data consistency (0s to disable).")
	flag.DurationVar(&daemonConsistencyFlag, "C", time.Second, "With daemon mode, duration to bring back collected values for data consistency (shorthand).")

	flag.StringVar(&pidFile, "pidfile", "", "the pid file")
}

func main() {
	flag.Parse() // Scan the arguments list

	if versionFlag {
		fmt.Println("Version:", applicationVersion)
		return
	}

	if pidFile != "" {
		pid := strconv.Itoa(os.Getpid())
		if err := ioutil.WriteFile(pidFile, []byte(pid), 0644); err != nil {
			log.WithError(err).Panic("Unable to create pidfile\n")
		}
	}

	if daemonConsistencyFlag.Seconds() > 0 {
		consistencyFactor = daemonConsistencyFlag.Seconds() / daemonIntervalFlag.Seconds()
	}

	// Fill InfluxDB connection settings
	dbClient := newDBClient()

	// Build collect list
	collectList := buildCollectionList()

	collectionLoop(collectList, dbClient)

}

func collectionLoop(collectList []collectionFunc, client influx.Client) {
	ch := make(chan collectionResult, len(collectList))
	// Without daemon mode, do at least one lap
	first := true
	for first || daemonFlag {
		first = false

		// Collect data
		var data []*influx.Point

		for _, cl := range collectList {
			go cl(ch)
		}

		for i := len(collectList); i > 0; i-- {
			res := <-ch
			if res.err != nil {
				log.WithError(res.err).Error("Error collecting points.")
			} else if len(res.point) > 0 {
				for _, v := range res.point {
					if v != nil {
						data = append(data, v)
					} else {
						// Loop if we haven't all data:
						// Since diffed data didn't respond the
						// first time they are collected, loop
						// one more time to have it
						first = true
					}
				}
			} else {
				first = true
			}
		}

		if !first {
			// Show data
			if !first && (databaseFlag == "" || verboseFlag) {
				for _, j := range data {
					fmt.Printf("%s\n", j.String())
				}
			}

			// Send data
			if client != nil {
				if err := send(client, data); err != nil {
					log.WithError(err).Error("Error while sending data to influx db.")
				}
			}
		}

		if daemonFlag || first {
			time.Sleep(daemonIntervalFlag)
		}
	}
}

func newDBClient() influx.Client {
	var client influx.Client
	if databaseFlag != "" {
		var proto string
		if sslFlag {
			proto = "https"
		} else {
			proto = "http"
		}
		var u, _ = url.Parse(fmt.Sprintf("%s://%s/", proto, hostFlag))
		config := influx.HTTPConfig{Addr: u.String(), Username: usernameFlag, UserAgent: "sysinfo_influxdb v" + applicationVersion}

		// use secret file if present, fallback to CLI password arg
		if secretFlag != "" {
			data, err := ioutil.ReadFile(secretFlag)
			if err != nil {
				log.Panic(err)
			}
			config.Password = strings.Split(string(data), "\n")[0]
		} else {
			config.Password = passwordFlag
		}

		var err error
		client, err = influx.NewHTTPClient(config)
		if err != nil {
			log.Panic(err)
		}

		ti, s, err := client.Ping(time.Second)
		if err != nil {
			log.Panic(err)
		}

		log.Infof("Connected to: %s; ping: %d; version: %s", u, ti, s)
	}
	return client
}

/**
 * Interactions with InfluxDB
 */

func buildCollectionList() []collectionFunc {
	var collectList []collectionFunc
	for _, c := range strings.Split(collectFlag, ",") {
		switch strings.Trim(c, " ") {
		case "cpu":
			collectList = append(collectList, cpu)
		case "cpus":
			collectList = append(collectList, cpus)
		case "mem":
			collectList = append(collectList, mem)
		case "swap":
			collectList = append(collectList, swap)
		case "uptime":
			collectList = append(collectList, uptime)
		case "load":
			collectList = append(collectList, load)
		case "network":
			collectList = append(collectList, network)
		case "disks":
			collectList = append(collectList, disks)
		case "mounts":
			collectList = append(collectList, mounts)
		default:
			log.Panicf("Unknown collect option `%s'\n", c)
			return nil
		}
	}
	return collectList
}

/**
 * Diff function
 */

func send(client influx.Client, series []*influx.Point) error {
	c := influx.BatchPointsConfig{Database: databaseFlag, RetentionPolicy: retentionPolicyFlag}
	w, _ := influx.NewBatchPoints(c)

	w.AddPoints(series)

	err := client.Write(w)
	return err
}

var (
	mutex      sync.Mutex
	lastSeries = make(map[string]map[string]interface{})
)

func diffFromLast(point *influx.Point) *influx.Point {
	mutex.Lock()
	defer mutex.Unlock()
	notComplete := false

	var keys []string

	for k := range point.Tags() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	key := point.Name() + "#"
	for _, k := range keys {
		key += k + ":" + point.Tags()[k] + "|"
	}

	if _, ok := lastSeries[key]; !ok {
		lastSeries[key] = make(map[string]interface{})
	}

	fields, err := point.Fields()
	if err != nil {
		log.WithError(err).Error("Cannot read fields.")
	}

	for i := range fields {
		var val interface{}
		var ok bool
		if val, ok = lastSeries[key][i]; !ok {
			notComplete = true
			lastSeries[key][i] = fields[i]
			continue
		} else {
			lastSeries[key][i] = fields[i]
		}

		switch fields[i].(type) {
		case int8:
			fields[i] = int8(float64(fields[i].(int8)-val.(int8)) * consistencyFactor)
		case int16:
			fields[i] = int16(float64(fields[i].(int16)-val.(int16)) * consistencyFactor)
		case int32:
			fields[i] = int32(float64(fields[i].(int32)-val.(int32)) * consistencyFactor)
		case int64:
			fields[i] = int64(float64(fields[i].(int64)-val.(int64)) * consistencyFactor)
		case uint8:
			fields[i] = uint8(float64(fields[i].(uint8)-val.(uint8)) * consistencyFactor)
		case uint16:
			fields[i] = uint16(float64(fields[i].(uint16)-val.(uint16)) * consistencyFactor)
		case uint32:
			fields[i] = uint32(float64(fields[i].(uint32)-val.(uint32)) * consistencyFactor)
		case uint64:
			fields[i] = uint64(float64(fields[i].(uint64)-val.(uint64)) * consistencyFactor)
		case int:
			fields[i] = int(float64(fields[i].(int)-val.(int)) * consistencyFactor)
		case uint:
			fields[i] = uint(float64(fields[i].(uint)-val.(uint)) * consistencyFactor)
		}
	}

	if notComplete {
		return nil
	} /*else*/
	return point

}

func cpu(ch chan collectionResult) {
	cpu := sigar.Cpu{}
	if err := cpu.Get(); err != nil {
		ch <- collectionResult{nil, err}
		return
	}

	series := newPoint(
		"cpu",
		map[string]string{
			"cpuid": "all",
		},
		map[string]interface{}{
			"user":  cpu.User,
			"nice":  cpu.Nice,
			"sys":   cpu.Sys,
			"idle":  cpu.Idle,
			"wait":  cpu.Wait,
			"total": cpu.Total(),
		},
	)

	ch <- collectionResult{[]*influx.Point{diffFromLast(series)}, nil}
}

/**
 * Gathering functions
 */

func cpus(ch chan collectionResult) {
	var series []*influx.Point

	cpus := sigar.CpuList{}
	cpus.Get()
	for i, cpu := range cpus.List {
		serie := newPoint(
			"cpus",
			map[string]string{
				"cpuid": fmt.Sprint(i),
			},
			map[string]interface{}{
				"user":  cpu.User,
				"nice":  cpu.Nice,
				"sys":   cpu.Sys,
				"idle":  cpu.Idle,
				"wait":  cpu.Wait,
				"total": cpu.Total(),
			},
		)

		if serie = diffFromLast(serie); serie != nil {
			series = append(series, serie)
		}
	}

	ch <- collectionResult{series, nil}
}

func mem(ch chan collectionResult) {
	mem := sigar.Mem{}
	if err := mem.Get(); err != nil {
		ch <- collectionResult{nil, err}
	}

	series := newPoint(
		"mem",
		map[string]string{},
		map[string]interface{}{
			"free":       mem.Free,
			"used":       mem.Used,
			"actualfree": mem.ActualFree,
			"actualused": mem.ActualUsed,
			"total":      mem.Total,
		},
	)

	ch <- collectionResult{[]*influx.Point{series}, nil}
}

func swap(ch chan collectionResult) {
	swap := sigar.Swap{}
	if err := swap.Get(); err != nil {
		ch <- collectionResult{nil, err}
		return
	}

	series := newPoint(
		"swap",
		map[string]string{},
		map[string]interface{}{
			"free":  swap.Free,
			"used":  swap.Used,
			"total": swap.Total,
		},
	)

	ch <- collectionResult{[]*influx.Point{series}, nil}
}

func uptime(ch chan collectionResult) {
	uptime := sigar.Uptime{}
	if err := uptime.Get(); err != nil {
		ch <- collectionResult{nil, err}
		return
	}

	serie := newPoint(
		"uptime",
		map[string]string{},
		map[string]interface{}{
			"length": uptime.Length,
		},
	)

	ch <- collectionResult{[]*influx.Point{serie}, nil}
}

func load(ch chan collectionResult) {
	load := sigar.LoadAverage{}
	if err := load.Get(); err != nil {
		ch <- collectionResult{nil, err}
		return
	}

	series := newPoint(
		"load",
		map[string]string{},
		map[string]interface{}{
			"one":     load.One,
			"five":    load.Five,
			"fifteen": load.Fifteen,
		},
	)

	ch <- collectionResult{[]*influx.Point{series}, nil}
}

func network(ch chan collectionResult) {
	fi, err := os.Open("/proc/net/dev")
	if err != nil {
		ch <- collectionResult{nil, err}
		return
	}
	defer fi.Close()

	var series []*influx.Point

	cols := []string{"recv_bytes", "recv_packets", "recv_errs", "recv_drop",
		"recv_fifo", "recv_frame", "recv_compressed",
		"recv_multicast", "trans_bytes", "trans_packets",
		"trans_errs", "trans_drop", "trans_fifo",
		"trans_colls", "trans_carrier", "trans_compressed"}

	// Search interface
	skip := 2
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		// Skip headers
		if skip > 0 {
			skip--
			continue
		}

		line := scanner.Text()
		tmp := strings.Split(line, ":")
		if len(tmp) < 2 {
			ch <- collectionResult{nil, nil}
			return
		}

		tmpf := strings.Fields(tmp[1])
		fields := map[string]interface{}{}
		for i, vc := range cols {
			if vt, err := strconv.Atoi(tmpf[i]); err == nil {
				fields[vc] = vt
			} else {
				fields[vc] = 0
			}
		}

		serie := newPoint(
			"network",
			map[string]string{
				"iface": strings.Trim(tmp[0], " "),
			},
			fields,
		)

		if serie = diffFromLast(serie); serie != nil {
			series = append(series, serie)
		}
	}

	ch <- collectionResult{series, nil}
}

func disks(ch chan collectionResult) {
	fi, err := os.Open("/proc/diskstats")
	if err != nil {
		ch <- collectionResult{nil, err}
		return
	}
	defer fi.Close()

	var series []*influx.Point

	cols := []string{"read_ios", "read_merges", "read_sectors", "read_ticks",
		"write_ios", "write_merges", "write_sectors", "write_ticks",
		"in_flight", "io_ticks", "time_in_queue"}

	// Search device
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		tmp := strings.Fields(scanner.Text())
		if len(tmp) < 14 {
			ch <- collectionResult{nil, nil}
			return
		}

		fields := map[string]interface{}{}
		for i, vc := range cols {
			if vt, err := strconv.Atoi(tmp[3+i]); err == nil {
				fields[vc] = vt
			} else {
				fields[vc] = 0
			}
		}

		point := newPoint(
			"disks",
			map[string]string{
				"device": strings.Trim(tmp[2], " "),
			},
			fields,
		)

		if point = diffFromLast(point); point != nil {
			series = append(series, point)
		}
	}

	ch <- collectionResult{series, nil}
}

func mounts(ch chan collectionResult) {
	fi, err := os.Open("/proc/mounts")
	if err != nil {
		ch <- collectionResult{nil, err}
		return
	}
	defer fi.Close()

	var series []*influx.Point

	// Exclude virtual & system fstype
	sysfs := []string{"binfmt_misc", "cgroup", "configfs", "debugfs",
		"devpts", "devtmpfs", "efivarfs", "fusectl", "mqueue",
		"none", "proc", "rootfs", "securityfs", "sysfs",
		"rpc_pipefs", "fuse.gvfsd-fuse", "tmpfs"}

	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		tmp := strings.Fields(scanner.Text())

		// Some hack needed to remove "none" virtual mountpoints
		if (stringInSlice(tmp[2], sysfs) == false) && (tmp[0] != "none") {
			fs := syscall.Statfs_t{}

			err := syscall.Statfs(tmp[1], &fs)
			if err != nil {
				ch <- collectionResult{nil, err}
				return
			}

			serie := newPoint(
				"mounts",
				map[string]string{
					"disk":       tmp[0],
					"mountpoint": tmp[1],
				},
				map[string]interface{}{
					"free":  fs.Bfree * uint64(fs.Bsize),
					"total": fs.Blocks * uint64(fs.Bsize),
				},
			)

			if serie = diffFromLast(serie); serie != nil {
				series = append(series, serie)
			}
		}
	}

	ch <- collectionResult{series, nil}
}

func getFqdn() string {
	// Note: We use exec here instead of os.Hostname() because we
	// want the FQDN, and this is the easiest way to get it.
	fqdn, err := exec.Command("hostname", "-f").Output()

	// Fallback to unqualifed name
	if err != nil {
		hostname, _ := os.Hostname()
		return hostname
	}

	return strings.TrimSpace(string(fqdn))
}

// "in_array" style func for strings
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func newPoint(name string, tags map[string]string, fields map[string]interface{}) *influx.Point {

	tags["fqdn"] = getFqdn()

	point, err := influx.NewPoint(
		name,
		tags,
		fields,
		time.Now(),
	)

	if err != nil {
		log.WithError(err).Error("Cannot create new point.")
	}

	return point
}
