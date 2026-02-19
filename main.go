package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "moi-si/lumine v0.7.0")
		fmt.Fprintln(os.Stderr)
		flag.PrintDefaults()
	}
	configPath := flag.String("c", "config.json", "Config file path")
	addr := flag.String("b", "", "SOCKS5 bind address (default: address from config file)")
	hAddr := flag.String("hb", "", "HTTP bind address (default: address from config file)")
	enableSystemTray := flag.Bool("gui", false, "Run with Windows system tray icon (Windows only)")

	flag.Parse()

	if *enableSystemTray {
		// Load config to get the proxy addresses for the system tray
		socks5Addr, httpAddr, err := loadConfig(*configPath)
		if err != nil {
			fmt.Println("Failed to load config:", err)
			return
		}

		// Override with command line flags if provided
		if *addr != "" {
			socks5Addr = *addr
		}
		if *hAddr != "" {
			httpAddr = *hAddr
		}

		// Run with system tray, passing the proxy addresses
		runWithSystray(socks5Addr, httpAddr, func() {
			startServer(*configPath, addr, hAddr)
		})
	} else {
		// Run in console mode
		startServer(*configPath, addr, hAddr)
	}
}

func startServer(configPath string, addr, hAddr *string) {
	socks5Addr, httpAddr, err := loadConfig(configPath)
	if err != nil {
		fmt.Println("Failed to load config:", err)
		return
	}

	if len(ipPools) != 0 {
		for _, pool := range ipPools {
			defer pool.Close()
		}
	}

	done := make(chan struct{})
	go socks5Accept(addr, socks5Addr, done)
	httpAccept(hAddr, httpAddr)
	<-done
}
