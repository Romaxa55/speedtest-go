package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/romaxa55/speedtest-go/speedtest"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	showList      = kingpin.Flag("list", "Show available speedtest.net servers.").Short('l').Bool()
	serverIds     = kingpin.Flag("server", "Select server id to run speedtest.").Short('s').Ints()
	customURL     = kingpin.Flag("custom-url", "Specify the url of the server instead of getting a list from speedtest.net.").String()
	jsonOutput    = kingpin.Flag("json", "Output results in json format.").Bool()
	location      = kingpin.Flag("location", "Change the location with a precise coordinate. Format: lat,lon").String()
	city          = kingpin.Flag("city", "Change the location with a predefined city label.").String()
	showCityList  = kingpin.Flag("city-list", "List all predefined city labels.").Bool()
	proxy         = kingpin.Flag("proxy", "Set a proxy(http[s] or socks) for the speedtest.").String()
	source        = kingpin.Flag("source", "Bind a source interface for the speedtest.").String()
	multi         = kingpin.Flag("multi", "Enable multi-server mode.").Short('m').Bool()
	thread        = kingpin.Flag("thread", "Set the number of concurrent connections.").Short('t').Int()
	search        = kingpin.Flag("search", "Fuzzy search servers by a keyword.").String()
	noDownload    = kingpin.Flag("no-download", "Disable download test.").Bool()
	noUpload      = kingpin.Flag("no-upload", "Disable upload test.").Bool()
	forceHTTPPing = kingpin.Flag("force-http-ping", "Force ping using http.").Bool()
	debug         = kingpin.Flag("debug", "Enable debug mode.").Short('d').Bool()
)

func main() {

	kingpin.Version(speedtest.Version())
	kingpin.Parse()

	// 0. speed test setting
	var speedtestClient = speedtest.New(speedtest.WithUserConfig(
		&speedtest.UserConfig{
			UserAgent:    speedtest.DefaultUserAgent,
			Proxy:        *proxy,
			Source:       *source,
			Debug:        *debug,
			ICMP:         (os.Geteuid() == 0 || os.Geteuid() == -1) && len(*proxy) == 0 && !*forceHTTPPing, // proxy may not support ICMP
			SavingMode:   true,
			CityFlag:     *city,
			LocationFlag: *location,
			Keyword:      *search,
			NoDownload:   *noDownload,
			NoUpload:     *noUpload,
		}))
	speedtestClient.SetNThread(*thread)

	if *showCityList {
		speedtest.PrintCityList()
		return
	}

	// 1. retrieving user information
	taskManager := InitTaskManager(!*jsonOutput)
	taskManager.AsyncRun("Retrieving User Information", func(task *Task) {
		u, err := speedtestClient.FetchUserInfo()
		task.CheckError(err)
		task.Printf("ISP: %s", u.String())
		task.Complete()
	})

	// 2. retrieving servers
	var err error
	var servers speedtest.Servers
	var targets speedtest.Servers
	taskManager.Run("Retrieving Servers", func(task *Task) {
		if len(*customURL) > 0 {
			var target *speedtest.Server
			target, err = speedtestClient.CustomServer(*customURL)
			task.CheckError(err)
			targets = []*speedtest.Server{target}
			task.Println("Skip: Using Custom Server")
		} else {
			servers, err = speedtestClient.FetchServers()
			task.CheckError(err)
			task.Printf("Found %d Public Servers", len(servers))
			if *showList {
				task.Complete()
				task.manager.Reset()
				showServerList(servers)
				os.Exit(1)
			}
			targets, err = servers.FindServer(*serverIds)
			task.CheckError(err)
		}
		task.Complete()
	})
	taskManager.Reset()

	// 3. test each selected server with ping, download and upload.
	for _, server := range targets {
		if !*jsonOutput {
			fmt.Println()
		}
		taskManager.Println("Test Server: " + server.String())
		taskManager.Run("Latency: ", func(task *Task) {
			task.CheckError(server.PingTest(func(latency time.Duration) {
				task.Printf("Latency: %v", latency)
			}))
			task.Printf("Latency: %v Jitter: %v Min: %v Max: %v", server.Latency, server.Jitter, server.MinLatency, server.MaxLatency)
			task.Complete()
		})

		taskManager.Run("Download", func(task *Task) {
			ticker := speedtestClient.CallbackDownloadRate(func(downRate float64) {
				task.Printf("Download: %.2fMbps", downRate)
			})
			if *multi {
				task.CheckError(server.MultiDownloadTestContext(context.Background(), servers))
			} else {
				task.CheckError(server.DownloadTest())
			}
			ticker.Stop()
			task.Printf("Download: %.2fMbps (used: %.2fMB)", server.DLSpeed, float64(server.Context.Manager.GetTotalDownload())/1024/1024)
			task.Complete()
		})

		taskManager.Run("Upload", func(task *Task) {
			ticker := speedtestClient.CallbackUploadRate(func(upRate float64) {
				task.Printf("Upload: %.2fMbps", upRate)
			})
			if *multi {
				task.CheckError(server.MultiUploadTestContext(context.Background(), servers))
			} else {
				task.CheckError(server.UploadTest())
			}
			ticker.Stop()
			task.Printf("Upload: %.2fMbps (used: %.2fMB)", server.ULSpeed, float64(server.Context.Manager.GetTotalUpload())/1024/1024)
			task.Complete()
		})
		taskManager.Reset()
		speedtestClient.Manager.Reset()
	}

	taskManager.Stop()

	if !*jsonOutput {
		json, errMarshal := speedtestClient.JSON(targets)
		if errMarshal != nil {
			return
		}
		fmt.Println("Saved to /tmp/speedtest.json")
		// Сохранение в файл
		err := os.WriteFile("/tmp/speedtest.json", json, 0644)
		if err != nil {
			fmt.Println("Ошибка при сохранении в файл:", err)
		}
	}
}

func showServerList(servers speedtest.Servers) {
	for _, s := range servers {
		fmt.Printf("[%5s] %9.2fkm ", s.ID, s.Distance)

		if s.Latency == -1 {
			fmt.Printf("%v", "Timeout ")
		} else {
			fmt.Printf("%-dms ", s.Latency/time.Millisecond)
		}
		fmt.Printf("\t%s (%s) by %s \n", s.Name, s.Country, s.Sponsor)
	}
}
