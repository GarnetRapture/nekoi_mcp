package main

import "fmt"

// Authorship and contact, compiled into the binary so a distributed copy
// carries its origin with it.
const (
	AppName    = "Nekoi_MCP"
	AppVersion = "1.0.0"
	AppAuthor  = "Nekoi"
	AppEmail   = "garnet@everlib.pro"
	AppRepo    = "https://github.com/GarnetRapture"
)

func printAbout() int {
	fmt.Printf("%s %s\nAuthor: %s\nContact: %s\nRepository: %s\n",
		AppName, AppVersion, AppAuthor, AppEmail, AppRepo)
	return 0
}
