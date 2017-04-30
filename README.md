# influxdb_reporter

[![Build Status](https://travis-ci.org/ottenwbe/influxdb_reporter.svg?branch=master)](https://travis-ci.org/ottenwbe/influxdb_reporter)

A tool for collecting system metrics (CPU, memory, load, disks I/Os, network traffic) and reporting them to an [InfluxDB](http://influxdb.org) server.

This project mainly relies on [gosigar](https://github.com/cloudfoundry/gosigar/), so it's compatible with GNU/Linux and MacOS systems, but not with Windows.

## Original source

This is a fork of [sysinfo_influxdb](https://github.com/novaquark/sysinfo_influxdb), which has been licenced under the Creative Commons Zero v1.0 Universal license.
This fork is used to personalize this project for my usecase.

## Install ##

1. Ensure that [Go](https://golang.org/doc/install) is installed and the [GOPATH](https://golang.org/doc/code.html) is set. 
Moreover, we assume that `$GOPATH/bin` is in your `PATH`.

1. Go build the executable in `${GOPATH}/bin`:
    
    ```bash    
    go get github.com/ottenwbe/influxdb_reporter
    ```
1. Now you can execute the application by typing: 

    ```bash    
    golook --help

## Usage sample

To display all metrics once without sending them to a server :

    influxdb_reporter

To send metrics to an InfluxDB server, only once:

    influxdb_reporter -h localhost:8086 -u root -p secret -d database

The password can also be read from a file if you don't want to specify it via the CLI (`-p` is ignored if specified with `-s`) :

    influxdb_reporter -h localhost:8086 -u root -s /etc/sysinfo.secret -d database

You can ommit `-h`, `-u`, `-p` or `-s` if you use default settings.

To run in daemon mode (doesn't fork, just loop), use the `-D` option:

    influxdb_reporter -D

To display data even if you send them to a server, use `-v`:

    influxdb_reporter -D -h localhost:8086 -d database -v

Use the `-i` option to change the collection interval; this option preserves the consistency of quantities displayed or sent (CPUs, network, or disks I/Os): so you can store in the same table the amount of outgoing packets in 1 minute to the same amount outgoing in 1 second (use the `-C` option to alter the consistency factor). For example, to collect statistics each minute :

    influxdb_reporter -i 1m

To change data collected, use the `-c` option with one or more metrics type (`cpu`, `cpus`, `mem`, `swap`, `uptime`, `load`, `network`, `disks`, `mounts`) like this :

    influxdb_reporter -c cpus # Collect only CPU related statistics by CPU core
    influxdb_reporter -c load,cpu,disks # Collect load average, global CPU and disks I/Os statistics
    influxdb_reporter -c mem,mounts # Collect memory metrics and local filesystems usage

On a Linux hardened kernel, you must be allowed to read `/proc/net/dev` in order to collect networking statistics.

## Sample outputs

### CPU

	[
	  {
	    "measurement": "cpu",
	    "tags": {
	      "cpuid": "all",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "idle": 149,
	      "nice": 0,
	      "sys": 7,
	      "total": 190,
	      "user": 26,
	      "wait": 0
	    }
	  }
	]

### CPUs

	[
	  {
	    "measurement": "cpus",
	    "tags": {
	      "cpuid": "0",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "idle": 69,
	      "nice": 0,
	      "sys": 6,
	      "total": 95,
	      "user": 14,
	      "wait": 0
	    }
	  },
	  {
	    "measurement": "cpus",
	    "tags": {
	      "cpuid": "1",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "idle": 84,
	      "nice": 0,
	      "sys": 2,
	      "total": 97,
	      "user": 11,
	      "wait": 0
	    }
	  }
	]

### Memory

	[
	  {
	    "measurement": "mem",
	    "tags": {
	      "fqdn": "koala"
	    },
	    "fields": {
	      "actualfree": 460017664,
	      "actualused": 460308480,
	      "free": 153395200,
	      "total": 920326144,
	      "used": 766930944
	    }
	  }
	]

### Swap

	[
	  {
	    "measurement": "swap",
	    "tags": {
	      "fqdn": "koala"
	    },
	    "fields": {
	      "free": 2145689600,
	      "total": 2147479552,
	      "used": 1789952
	    }
	  }
	]

### Uptime

	[
	  {
	    "measurement": "uptime",
	    "tags": {
	      "fqdn": "koala"
	    },
	    "fields": {
	      "length": 154954
	    }
	  }
	]

### Load average

	[
	  {
	    "measurement": "load",
	    "tags": {
	      "fqdn": "koala"
	    },
	    "fields": {
	      "fifteen": 1.05,
	      "five": 1.05,
	      "one": 0.82
	    }
	  }
	]


### Network

	[
	  {
	    "measurement": "network",
	    "tags": {
	      "fqdn": "koala",
	      "iface": "lo"
	    },
	    "fields": {
	      "recv_bytes": 0,
	      "recv_compressed": 0,
	      "recv_drop": 0,
	      "recv_errs": 0,
	      "recv_fifo": 0,
	      "recv_frame": 0,
	      "recv_multicast": 0,
	      "recv_packets": 0,
	      "trans_bytes": 0,
	      "trans_carrier": 0,
	      "trans_colls": 0,
	      "trans_compressed": 0,
	      "trans_drop": 0,
	      "trans_errs": 0,
	      "trans_fifo": 0,
	      "trans_packets": 0
	    }
	  },
	  {
	    "measurement": "network",
	    "tags": {
	      "fqdn": "koala",
	      "iface": "eth0"
	    },
	    "fields": {
	      "recv_bytes": 156,
	      "recv_compressed": 0,
	      "recv_drop": 0,
	      "recv_errs": 0,
	      "recv_fifo": 0,
	      "recv_frame": 0,
	      "recv_multicast": 0,
	      "recv_packets": 2,
	      "trans_bytes": 206,
	      "trans_carrier": 0,
	      "trans_colls": 0,
	      "trans_compressed": 0,
	      "trans_drop": 0,
	      "trans_errs": 0,
	      "trans_fifo": 0,
	      "trans_packets": 3
	    }
	  }
	]

### Disks I/Os

	[
	  {
	    "measurement": "disks",
	    "tags": {
	      "device": "sda",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "in_flight": 0,
	      "io_ticks": 0,
	      "read_ios": 0,
	      "read_merges": 0,
	      "read_sectors": 0,
	      "read_ticks": 0,
	      "time_in_queue": 0,
	      "write_ios": 0,
	      "write_merges": 0,
	      "write_sectors": 0,
	      "write_ticks": 0
	    }
	  },
	  {
	    "measurement": "disks",
	    "tags": {
	      "device": "sda1",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "in_flight": 0,
	      "io_ticks": 0,
	      "read_ios": 0,
	      "read_merges": 0,
	      "read_sectors": 0,
	      "read_ticks": 0,
	      "time_in_queue": 0,
	      "write_ios": 0,
	      "write_merges": 0,
	      "write_sectors": 0,
	      "write_ticks": 0
	    }
	  },
	  {
	    "measurement": "disks",
	    "tags": {
	      "device": "sda2",
	      "fqdn": "koala"
	    },
	    "fields": {
	      "in_flight": 0,
	      "io_ticks": 0,
	      "read_ios": 0,
	      "read_merges": 0,
	      "read_sectors": 0,
	      "read_ticks": 0,
	      "time_in_queue": 0,
	      "write_ios": 0,
	      "write_merges": 0,
	      "write_sectors": 0,
	      "write_ticks": 0
	    }
	  }
	]

### Mountpoints

	[
	  {
	    "measurement": "mounts",
	    "tags": {
	      "disk": "/dev/root",
	      "fqdn": "koala",
	      "mountpoint": "/"
	    },
	    "fields": {
	      "free": 0,
	      "total": 0
	    }
	  },
	  {
	    "measurement": "mounts",
	    "tags": {
	      "disk": "/dev/sda2",
	      "fqdn": "koala",
	      "mountpoint": "/home"
	    },
	    "fields": {
	      "free": 0,
	      "total": 0
	    }
	  }
	]