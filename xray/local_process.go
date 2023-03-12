package xray

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"x-ui/util/common"

	"github.com/Workiva/go-datastructures/queue"
	statsservice "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
)

type localProcess struct {
	cmd *exec.Cmd

	version string
	apiPort int

	config  *Config
	lines   *queue.Queue
	exitErr error
}

type LocalProcess struct {
	*localProcess
}

func NewLocalProcess(xrayConfig *Config) Process {
	p := &LocalProcess{newLocalProcess(xrayConfig)}
	runtime.SetFinalizer(p, stopProcess)
	return p
}

func newLocalProcess(config *Config) *localProcess {
	return &localProcess{
		version: "Unknown",
		config:  config,
		lines:   queue.New(100),
	}
}

func (p *localProcess) IsRunning() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	if p.cmd.ProcessState == nil {
		return true
	}
	return false
}

func (p *localProcess) GetErr() error {
	return p.exitErr
}

func (p *localProcess) GetResult() string {
	if p.lines.Empty() && p.exitErr != nil {
		return p.exitErr.Error()
	}
	items, _ := p.lines.TakeUntil(func(item interface{}) bool {
		return true
	})
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.(string))
	}
	return strings.Join(lines, "\n")
}

func (p *localProcess) GetVersion() string {
	return p.version
}

func (p *localProcess) GetAPIPort() int {
	return p.apiPort
}

func (p *localProcess) GetConfig() *Config {
	return p.config
}

func (p *localProcess) refreshAPIPort() {
	for _, inbound := range p.config.InboundConfigs {
		if inbound.Tag == "api" {
			p.apiPort = inbound.Port
			break
		}
	}
}

func (p *localProcess) refreshVersion() {
	cmd := exec.Command(GetBinaryPath(), "-version")
	data, err := cmd.Output()
	if err != nil {
		p.version = "Unknown"
	} else {
		datas := bytes.Split(data, []byte(" "))
		if len(datas) <= 1 {
			p.version = "Unknown"
		} else {
			p.version = string(datas[1])
		}
	}
}

func (p *localProcess) Start() (err error) {
	if p.IsRunning() {
		return errors.New("xray is already running")
	}

	defer func() {
		if err != nil {
			p.exitErr = err
		}
	}()

	data, err := json.MarshalIndent(p.config, "", "  ")
	if err != nil {
		return common.NewErrorf("生成 xray 配置文件失败: %v", err)
	}
	configPath := GetConfigPath()
	err = os.WriteFile(configPath, data, fs.ModePerm)
	if err != nil {
		return common.NewErrorf("写入配置文件失败: %v", err)
	}

	cmd := exec.Command(GetBinaryPath(), "-c", configPath)
	p.cmd = cmd

	stdReader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	errReader, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	go func() {
		defer func() {
			common.Recover("")
			stdReader.Close()
		}()
		reader := bufio.NewReaderSize(stdReader, 8192)
		for {
			line, _, err := reader.ReadLine()
			if err != nil {
				return
			}
			if p.lines.Len() >= 100 {
				p.lines.Get(1)
			}
			p.lines.Put(string(line))
		}
	}()

	go func() {
		defer func() {
			common.Recover("")
			errReader.Close()
		}()
		reader := bufio.NewReaderSize(errReader, 8192)
		for {
			line, _, err := reader.ReadLine()
			if err != nil {
				return
			}
			if p.lines.Len() >= 100 {
				p.lines.Get(1)
			}
			p.lines.Put(string(line))
		}
	}()

	go func() {
		err := cmd.Run()
		if err != nil {
			p.exitErr = err
		}
	}()

	p.refreshVersion()
	p.refreshAPIPort()

	return nil
}

func (p *localProcess) Stop() error {
	if !p.IsRunning() {
		return errors.New("xray is not running")
	}
	return p.cmd.Process.Kill()
}

func (p *localProcess) GetTraffic(reset bool) ([]*Traffic, []*ClientTraffic, error) {
	if p.apiPort == 0 {
		return nil, nil, common.NewError("xray api port wrong:", p.apiPort)
	}
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%v", p.apiPort), grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	client := statsservice.NewStatsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	request := &statsservice.QueryStatsRequest{
		Reset_: reset,
	}
	resp, err := client.QueryStats(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	tagTrafficMap := map[string]*Traffic{}
	emailTrafficMap := map[string]*ClientTraffic{}

	clientTraffics := make([]*ClientTraffic, 0)
	traffics := make([]*Traffic, 0)
	for _, stat := range resp.GetStat() {
		matchs := trafficRegex.FindStringSubmatch(stat.Name)
		if len(matchs) < 3 {

			matchs := ClientTrafficRegex.FindStringSubmatch(stat.Name)
			if len(matchs) < 3 {
				continue
			} else {

				isUser := matchs[1] == "user"
				email := matchs[2]
				isDown := matchs[3] == "downlink"
				if !isUser {
					continue
				}
				traffic, ok := emailTrafficMap[email]
				if !ok {
					traffic = &ClientTraffic{
						Email: email,
					}
					emailTrafficMap[email] = traffic
					clientTraffics = append(clientTraffics, traffic)
				}
				if isDown {
					traffic.Down = stat.Value
				} else {
					traffic.Up = stat.Value
				}

			}
			continue
		}
		isInbound := matchs[1] == "inbound"
		tag := matchs[2]
		isDown := matchs[3] == "downlink"
		if tag == "api" {
			continue
		}
		traffic, ok := tagTrafficMap[tag]
		if !ok {
			traffic = &Traffic{
				IsInbound: isInbound,
				Tag:       tag,
			}
			tagTrafficMap[tag] = traffic
			traffics = append(traffics, traffic)
		}
		if isDown {
			traffic.Down = stat.Value
		} else {
			traffic.Up = stat.Value
		}
	}

	return traffics, clientTraffics, nil
}
