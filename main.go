/*
MIT License

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
	influxClient "github.com/influxdata/influxdb/client/v2"
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

type chan_ret_t struct {
	series []*influxClient.Point
	err    error
}

const APP_VERSION = "0.6.0"

// Variables storing arguments flags
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
		fmt.Println("Version:", APP_VERSION)
		return
	}

	if pidFile != "" {
		pid := strconv.Itoa(os.Getpid())
		if err := ioutil.WriteFile(pidFile, []byte(pid), 0644); err != nil {
			log.WithError(err).Panic(os.Stderr, "Unable to create pidfile\n")
		}
	}

	if daemonConsistencyFlag.Seconds() > 0 {
		consistencyFactor = daemonConsistencyFlag.Seconds() / daemonIntervalFlag.Seconds()
	}

	// Fill InfluxDB connection settings
	client := newClient()

	// Build collect list
	collectList := buildCollectionList()

	collectionLoop(collectList, client)

}

func collectionLoop(collectList []GatherFunc, client influxClient.Client) {
	ch := make(chan chan_ret_t, len(collectList))
	// Without daemon mode, do at least one lap
	first := true
	for first || daemonFlag {
		first = false

		// Collect data
		var data []*influxClient.Point

		for _, cl := range collectList {
			go cl(ch)
		}

		for i := len(collectList); i > 0; i-- {
			res := <-ch
			if res.err != nil {
				log.WithError(res.err).Error("Error collecting points.")
			} else if len(res.series) > 0 {
				for _, v := range res.series {
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

func newClient() influxClient.Client {
	var client influxClient.Client = nil
	if databaseFlag != "" {
		var proto string
		if sslFlag {
			proto = "https"
		} else {
			proto = "http"
		}
		var u, _ = url.Parse(fmt.Sprintf("%s://%s/", proto, hostFlag))
		config := influxClient.HTTPConfig{Addr: u.String(), Username: usernameFlag, UserAgent: "sysinfo_influxdb v" + APP_VERSION}

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
		client, err = influxClient.NewHTTPClient(config)
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

func buildCollectionList() []GatherFunc {
	var collectList []GatherFunc
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
 * Interactions with InfluxDB
 */

func send(client influxClient.Client, series []*influxClient.Point) error {
	c := influxClient.BatchPointsConfig{Database: databaseFlag, RetentionPolicy: retentionPolicyFlag}
	w, _ := influxClient.NewBatchPoints(c)

	w.AddPoints(series)

	err := client.Write(w)
	return err
}

/**
 * Diff function
 */

var (
	mutex       sync.Mutex
	last_series = make(map[string]map[string]interface{})
)

func DiffFromLast(serie *influxClient.Point) *influxClient.Point {
	mutex.Lock()
	defer mutex.Unlock()
	notComplete := false

	var keys []string

	for k := range serie.Tags() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	key := serie.Name() + "#"
	for _, k := range keys {
		key += k + ":" + serie.Tags()[k] + "|"
	}

	if _, ok := last_series[key]; !ok {
		last_series[key] = make(map[string]interface{})
	}

	fields, err := serie.Fields()
	if err != nil {
		log.WithError(err).Error("Cannot read fields")
	}

	for i := range fields {
		var val interface{}
		var ok bool
		if val, ok = last_series[key][i]; !ok {
			notComplete = true
			last_series[key][i] = fields[i]
			continue
		} else {
			last_series[key][i] = fields[i]
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
	} else {
		return serie
	}
}

type GatherFunc func(chan chan_ret_t)

func cpu(ch chan chan_ret_t) {
	cpu := sigar.Cpu{}
	if err := cpu.Get(); err != nil {
		ch <- chan_ret_t{nil, err}
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

	ch <- chan_ret_t{[]*influxClient.Point{DiffFromLast(series)}, nil}
}

/**
 * Gathering functions
 */

func cpus(ch chan chan_ret_t) {
	var series []*influxClient.Point

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

		if serie = DiffFromLast(serie); serie != nil {
			series = append(series, serie)
		}
	}

	ch <- chan_ret_t{series, nil}
}

func mem(ch chan chan_ret_t) {
	mem := sigar.Mem{}
	if err := mem.Get(); err != nil {
		ch <- chan_ret_t{nil, err}
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

	ch <- chan_ret_t{[]*influxClient.Point{series}, nil}
}

func swap(ch chan chan_ret_t) {
	swap := sigar.Swap{}
	if err := swap.Get(); err != nil {
		ch <- chan_ret_t{nil, err}
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

	ch <- chan_ret_t{[]*influxClient.Point{series}, nil}
}

func uptime(ch chan chan_ret_t) {
	uptime := sigar.Uptime{}
	if err := uptime.Get(); err != nil {
		ch <- chan_ret_t{nil, err}
		return
	}

	serie := newPoint(
		"uptime",
		map[string]string{},
		map[string]interface{}{
			"length": uptime.Length,
		},
	)

	ch <- chan_ret_t{[]*influxClient.Point{serie}, nil}
}

func load(ch chan chan_ret_t) {
	load := sigar.LoadAverage{}
	if err := load.Get(); err != nil {
		ch <- chan_ret_t{nil, err}
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

	ch <- chan_ret_t{[]*influxClient.Point{series}, nil}
}

func network(ch chan chan_ret_t) {
	fi, err := os.Open("/proc/net/dev")
	if err != nil {
		ch <- chan_ret_t{nil, err}
		return
	}
	defer fi.Close()

	var series []*influxClient.Point

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
			ch <- chan_ret_t{nil, nil}
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

		if serie = DiffFromLast(serie); serie != nil {
			series = append(series, serie)
		}
	}

	ch <- chan_ret_t{series, nil}
}

func disks(ch chan chan_ret_t) {
	fi, err := os.Open("/proc/diskstats")
	if err != nil {
		ch <- chan_ret_t{nil, err}
		return
	}
	defer fi.Close()

	var series []*influxClient.Point

	cols := []string{"read_ios", "read_merges", "read_sectors", "read_ticks",
		"write_ios", "write_merges", "write_sectors", "write_ticks",
		"in_flight", "io_ticks", "time_in_queue"}

	// Search device
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		tmp := strings.Fields(scanner.Text())
		if len(tmp) < 14 {
			ch <- chan_ret_t{nil, nil}
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

		if point = DiffFromLast(point); point != nil {
			series = append(series, point)
		}
	}

	ch <- chan_ret_t{series, nil}
}

func mounts(ch chan chan_ret_t) {
	fi, err := os.Open("/proc/mounts")
	if err != nil {
		ch <- chan_ret_t{nil, err}
		return
	}
	defer fi.Close()

	var series []*influxClient.Point

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
				ch <- chan_ret_t{nil, err}
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

			if serie = DiffFromLast(serie); serie != nil {
				series = append(series, serie)
			}
		}
	}

	ch <- chan_ret_t{series, nil}
}

func getFqdn() string {
	// Note: We use exec here instead of os.Hostname() because we
	// want the FQDN, and this is the easiest way to get it.
	fqdn, err := exec.Command("hostname", "-f").Output()

	// Fallback on Unqualifed name
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

func newPoint(name string, tags map[string]string, fields map[string]interface{}) *influxClient.Point {

	tags["fqdn"] = getFqdn()

	point, err := influxClient.NewPoint(
		name,
		tags,
		fields,
		time.Now(),
	)

	if err != nil {
		log.WithError(err).Error("Cannot create new point")
	}

	return point
}
