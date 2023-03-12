package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"x-ui/logger"
	"x-ui/util/common"

	statsservice "github.com/xtls/xray-core/app/stats/command"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
)

type remoteProcess struct {
	version             string
	apiPort             int
	lastRunningStatus   bool
	lastStatusCheckTime time.Time
	host                string
	user                string
	password            string
	sshPort             int

	conn    *Connection
	config  *Config
	exitErr error
}

type RemoteProcess struct {
	*remoteProcess
}

func NewRemoteProcess(xrayConfig *Config, host, rootPass string) Process {
	p := &RemoteProcess{newRemoteProcess(xrayConfig)}
	p.host = host
	p.user = "root"
	p.password = rootPass
	p.sshPort = 22
	runtime.SetFinalizer(p, stopProcess)

	sshHost := p.host + ":" + strconv.Itoa(p.sshPort)
	conn, err := Connect(sshHost, p.user, p.password)
	if err != nil {
		logger.Error(err)
	}
	p.conn = conn
	return p
}

func newRemoteProcess(config *Config) *remoteProcess {
	return &remoteProcess{
		version: "Unknown",
		config:  config,
	}
}

func (p *remoteProcess) IsRunning() bool {

	// Check the status every 10 seconds
	if time.Now().Sub(p.lastStatusCheckTime) < time.Second*10 {
		return p.lastRunningStatus
	}
	logger.Warning("IsRunning")
	p.lastStatusCheckTime = time.Now()

	cmd := "ps -A -o pid,cmd | grep " + GetBinaryName()
	output, err := p.conn.SendCommands(cmd)
	if err != nil {
		logger.Error("Not running: ", err)
		p.exitErr = err
		p.lastRunningStatus = false
		return false
	}
	if strings.Contains(string(output), GetBinaryName()+" -c") {
		logger.Warning("output: ", string(output))
		p.lastRunningStatus = true
		return true
	} else {
		logger.Error("output: ", string(output))
	}

	p.lastRunningStatus = false
	return false
}

func (p *remoteProcess) GetErr() error {
	return p.exitErr
}

func (p *remoteProcess) GetResult() string {
	if p.exitErr != nil {
		return p.exitErr.Error()
	}
	return ""
}

func (p *remoteProcess) GetVersion() string {
	return p.version
}

func (p *remoteProcess) GetAPIPort() int {
	return p.apiPort
}

func (p *remoteProcess) GetConfig() *Config {
	return p.config
}

func (p *remoteProcess) refreshAPIPort() {
	for _, inbound := range p.config.InboundConfigs {
		if inbound.Tag == "api" {
			p.apiPort = inbound.Port
			break
		}
	}
}

func (p *remoteProcess) refreshVersion(conn *Connection) {

	cmd := "/usr/local/x-ui/" + GetBinaryPath() + " -version"
	output, err := conn.SendCommands(cmd)
	if err != nil {
		logger.Error(err)
		p.exitErr = err
	}
	re := regexp.MustCompile(`\d+(\.\d+)+`)
	p.version = re.FindString(string(output))
	logger.Warning("version: ", p.version)

}

func (p *remoteProcess) Start() error {
	// To make sure an old instance is not running
	p.Stop()

	data, err := json.MarshalIndent(p.config, "", "  ")
	if err != nil {
		return common.NewErrorf("Failed to generate xray configuration: %v", err)
	}

	path, err := os.Getwd()
	if err != nil {
		logger.Error(err)
		p.exitErr = err
		return err
	}

	var cmds []string
	cmds = append(cmds, "cd "+path)
	cmds = append(cmds, "rm -f "+GetConfigPath())
	cmds = append(cmds, "printf "+strconv.QuoteToASCII(string(data))+" >> "+GetConfigPath())
	output, err := p.conn.SendCommands(cmds...)
	if err != nil {
		logger.Error(err)
		p.exitErr = err
	} else {
		logger.Warning(string(output))
	}

	cmds = cmds[:0]
	cmds = append(cmds, "cd "+path)
	cmds = append(cmds, GetBinaryPath()+" -c "+GetConfigPath())
	err = p.conn.Start(cmds...)
	if err != nil {
		logger.Error(err)
		p.exitErr = err
	}

	p.refreshVersion(p.conn)
	p.refreshAPIPort()
	p.lastStatusCheckTime = time.Now().Add(-time.Hour * 1)
	logger.Info("Successfully started!")
	return nil
}

func (p *remoteProcess) Stop() error {
	if !p.IsRunning() {
		return errors.New("xray is not running")
	}

	// TODO: For debugging, remove
	logger.Warning("Stopping .............")
	logger.Error(debug.Stack())

	cmd := "pkill -f " + GetBinaryName()
	output, err := p.conn.SendCommands(cmd)
	if err != nil {
		logger.Error(err)
		p.exitErr = err
		return err
	} else {
		logger.Error(string(output))
	}
	return nil
}

func (p *remoteProcess) GetTraffic(reset bool) ([]*Traffic, []*ClientTraffic, error) {
	if p.apiPort == 0 {
		return nil, nil, common.NewError("xray api port wrong:", p.apiPort)
	}
	conn, err := grpc.Dial(fmt.Sprintf(p.host+":8080"), grpc.WithInsecure())
	if err != nil {
		logger.Error("grpc error: ", err)
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

type Connection struct {
	*ssh.Client
	addr     string
	user     string
	password string
}

func Connect(addr, user, password string) (*Connection, error) {
	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }),
	}

	conn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, err
	}
	return &Connection{conn, addr, user, password}, nil
}

func (conn *Connection) tryReconnect() error {
	tmpConn, err := Connect(conn.addr, conn.user, conn.password)
	if err != nil {
		return err
	}
	conn = tmpConn
	return err
}

func (conn *Connection) SendCommands(cmds ...string) ([]byte, error) {

	if conn == nil {
		return nil, errors.New("SSH connection closed!")
	}

	var session *ssh.Session
	var err error
	for i := 0; i < 2; i++ {
		session, err = conn.NewSession()
		if err != nil {
			logger.Error(err)
			conn.tryReconnect()
			continue
		}
		break
	}
	if err != nil {
		logger.Error(err)
		return []byte{}, err
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	err = session.RequestPty("xterm", 80, 40, modes)
	if err != nil {
		return []byte{}, err
	}

	// in, err := session.StdinPipe()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// out, err := session.StdoutPipe()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	var output []byte

	// go func(in io.WriteCloser, out io.Reader, output *[]byte) {
	// 	var (
	// 		line string
	// 		r    = bufio.NewReader(out)
	// 	)
	// 	for {
	// 		b, err := r.ReadByte()
	// 		if err != nil {
	// 			break
	// 		}

	// 		*output = append(*output, b)

	// 		if b == byte('\n') {
	// 			line = ""
	// 			continue
	// 		}

	// 		line += string(b)

	// 		if strings.HasPrefix(line, "[sudo] password for ") && strings.HasSuffix(line, ": ") {
	// 			_, err = in.Write([]byte(conn.password + "\n"))
	// 			if err != nil {
	// 				break
	// 			}
	// 		}
	// 	}
	// }(in, out, &output)

	cmd := strings.Join(cmds, "; ")
	output, err = session.Output(cmd)
	if err != nil {
		logger.Error("command error: ", err)
		return []byte{}, err
	}

	return output, nil

}

func (conn *Connection) Start(cmds ...string) error {
	if conn == nil {
		return errors.New("SSH connection closed!")
	}

	var session *ssh.Session
	var err error
	for i := 0; i < 2; i++ {
		session, err = conn.NewSession()
		if err != nil {
			logger.Error(err)
			conn.tryReconnect()
			continue
		}
		break
	}
	if err != nil {
		logger.Error(err)
		return err
	}
	defer session.Close()

	cmd := strings.Join(cmds, "; ")
	err = session.Start(cmd)
	if err != nil {
		return err
	}

	return nil

}
