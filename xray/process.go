package xray

import (
	"fmt"
	"regexp"
	"runtime"
)

type Process interface {
	Start() error
	Stop() error
	IsRunning() bool
	GetErr() error
	GetResult() string
	GetAPIPort() int
	GetConfig() *Config
	GetVersion() string
	GetTraffic(reset bool) ([]*Traffic, []*ClientTraffic, error)
}

func NewProcess(xrayConfig *Config, local bool) Process {
	var p Process
	if local {
		p = &LocalProcess{newLocalProcess(xrayConfig)}
	} else {
		p = &RemoteProcess{newRemoteProcess(xrayConfig)}
	}
	runtime.SetFinalizer(p, stopProcess)
	return p
}

var trafficRegex = regexp.MustCompile("(inbound|outbound)>>>([^>]+)>>>traffic>>>(downlink|uplink)")
var ClientTrafficRegex = regexp.MustCompile("(user)>>>([^>]+)>>>traffic>>>(downlink|uplink)")

func GetBinaryName() string {
	return fmt.Sprintf("xray-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func GetBinaryPath() string {
	return "bin/" + GetBinaryName()
}

func GetConfigPath() string {
	return "bin/config.json"
}

func GetGeositePath() string {
	return "bin/geosite.dat"
}

func GetGeoipPath() string {
	return "bin/geoip.dat"
}

func stopProcess(p Process) {
	p.Stop()
}
