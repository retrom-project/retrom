package runtimecontract

import "encoding/json"

type LaunchSession struct {
	ID           string
	Purpose      string
	Mode         string
	Title        string
	PlatformName string
	CoreName     string
	ReturnTo     string
	Warnings     []string
}

type LaunchInput struct {
	Binding       Binding
	Session       LaunchSession
	Resources     []json.RawMessage
	TargetOptions json.RawMessage
	Restore       json.RawMessage
	Netplay       json.RawMessage
}
