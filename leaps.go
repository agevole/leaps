/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/jeffail/leaps/lib"
	"github.com/jeffail/leaps/net"
	"github.com/jeffail/leaps/util"
)

/*--------------------------------------------------------------------------------------------------
 */

var (
	leapsVersion string
	dateBuilt    string
)

/*--------------------------------------------------------------------------------------------------
 */

/*
LeapsConfig - The all encompassing leaps configuration. Contains configurations for individual leaps
components, which determine the role of this leaps instance. Currently a stand alone leaps server is
the only supported role.
*/
type LeapsConfig struct {
	NumProcesses      int                    `json:"num_processes"`
	LoggerConfig      util.LoggerConfig      `json:"logger"`
	StatsConfig       util.StatsConfig       `json:"stats"`
	CuratorConfig     lib.CuratorConfig      `json:"curator"`
	HTTPServerConfig  net.HTTPServerConfig   `json:"http_server"`
	StatsServerConfig util.StatsServerConfig `json:"stats_server"`
}

/*--------------------------------------------------------------------------------------------------
 */

func main() {
	var (
		curator     net.LeapLocator
		err         error
		closeChan   = make(chan bool)
		showVersion = flag.Bool("v", false, "Display version info")
		configPath  = flag.String("c", "", "Path to a configuration file")
		leapsMode   = flag.String("m", "curator", "Leaps service mode, supports: curator, curator or curator")
	)

	flag.Parse()

	if *showVersion {
		fmt.Printf("Leaps version: %v\nDate: %v\n", leapsVersion, dateBuilt)
		return
	}

	rand.Seed(time.Now().Unix())

	leapsConfig := LeapsConfig{
		NumProcesses:      runtime.NumCPU(),
		LoggerConfig:      util.DefaultLoggerConfig(),
		StatsConfig:       util.DefaultStatsConfig(),
		CuratorConfig:     lib.DefaultCuratorConfig(),
		HTTPServerConfig:  net.DefaultHTTPServerConfig(),
		StatsServerConfig: util.DefaultStatsServerConfig(),
	}

	if len(*configPath) > 0 {
		// Read config file
		configBytes, err := ioutil.ReadFile(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Error reading config file: %v", err))
			return
		}
		if err = json.Unmarshal(configBytes, &leapsConfig); err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Error parsing config file: %v", err))
			return
		}
	}

	// We have our configuration, time to get started up
	if configJSON, err := json.MarshalIndent(leapsConfig, "", "	"); err == nil {
		fmt.Printf("Leaps server initializing, configuration:\n%v\n", string(configJSON))
		fmt.Printf("Launching a leaps instance, use CTRL+C to close.\n\n")
	} else {
		fmt.Fprintln(os.Stderr, fmt.Sprintf("Configuration marshal error: %v", err))
		return
	}

	runtime.GOMAXPROCS(leapsConfig.NumProcesses)

	logger := util.NewLogger(os.Stdout, leapsConfig.LoggerConfig)
	stats := util.NewStats(leapsConfig.StatsConfig)

	switch *leapsMode {
	case "curator":
		// We are running in curator node.
		curator, err = lib.NewCurator(leapsConfig.CuratorConfig, logger, stats)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Curator error: %v\n", err))
			return
		}
		leapHTTP, err := net.CreateHTTPServer(curator, leapsConfig.HTTPServerConfig, logger, stats, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("HTTP error: %v\n", err))
			return
		}
		go func() {
			if httperr := leapHTTP.Listen(); httperr != nil {
				fmt.Fprintln(os.Stderr, fmt.Sprintf("Http listen error: %v\n", httperr))
			}
			closeChan <- true
		}()
	default:
		fmt.Fprintln(os.Stderr, "Unrecognized mode, try --help (-h)")
		return
	}

	// Run a stats service in the background.
	statsServer, err := util.NewStatsServer(leapsConfig.StatsServerConfig, logger, stats)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Sprintf("Stats error: %v\n", err))
		return
	}
	go func() {
		if statserr := statsServer.Listen(); statserr != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Stats server listen error: %v\n", statserr))
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
	case <-closeChan:
	}

	curator.Close()
}

/*--------------------------------------------------------------------------------------------------
 */
