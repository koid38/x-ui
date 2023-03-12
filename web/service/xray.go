package service

import (
	"encoding/json"
	"errors"
	"sync"
	"x-ui/logger"
	"x-ui/xray"

	"go.uber.org/atomic"
)

var p = make([]xray.Process, 0)
var lock sync.Mutex
var isNeedXrayRestart atomic.Bool
var result string

type XrayService struct {
	inboundService InboundService
	settingService SettingService
}

func (s *XrayService) IsXrayRunning() bool {
	return len(p) > 0 && p[0].IsRunning()
}

func (s *XrayService) GetXrayErr() error {
	if len(p) == 0 {
		return nil
	}
	return p[0].GetErr()
}

func (s *XrayService) GetXrayResult() string {
	if result != "" {
		return result
	}
	if s.IsXrayRunning() {
		return ""
	}
	if len(p) == 0 {
		return ""
	}
	logger.Error("len(p):", len(p))
	result = p[0].GetResult()
	return result
}

func (s *XrayService) GetXrayVersion() string {
	if len(p) == 0 {
		return "Unknown"
	}
	return p[0].GetVersion()
}
func RemoveIndex(s []interface{}, index int) []interface{} {
	return append(s[:index], s[index+1:]...)
}

func (s *XrayService) GetXrayConfig() (*xray.Config, error) {
	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}

	xrayConfig := &xray.Config{}
	err = json.Unmarshal([]byte(templateConfig), xrayConfig)
	if err != nil {
		return nil, err
	}

	s.inboundService.DisableInvalidClients()

	inbounds, err := s.inboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if !inbound.Enable {
			continue
		}
		// get settings clients
		settings := map[string]interface{}{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		clients, ok := settings["clients"].([]interface{})
		if ok {
			// check users active or not

			clientStats := inbound.ClientStats
			for _, clientTraffic := range clientStats {

				for index, client := range clients {
					c := client.(map[string]interface{})
					if c["email"] == clientTraffic.Email {
						if !clientTraffic.Enable {
							clients = RemoveIndex(clients, index)
							logger.Info("Remove Inbound User", c["email"], "due the expire or traffic limit")

						}

					}
				}

			}
			settings["clients"] = clients
			modifiedSettings, err := json.Marshal(settings)
			if err != nil {
				return nil, err
			}

			inbound.Settings = string(modifiedSettings)
		}
		inboundConfig := inbound.GenXrayInboundConfig()
		xrayConfig.InboundConfigs = append(xrayConfig.InboundConfigs, *inboundConfig)
	}
	return xrayConfig, nil
}

func (s *XrayService) GetXrayTraffic() ([]*xray.Traffic, []*xray.ClientTraffic, error) {
	if !s.IsXrayRunning() {
		return nil, nil, errors.New("xray is not running")
	}
	return p[0].GetTraffic(true)
}

func (s *XrayService) RestartXray(isForce bool) error {
	lock.Lock()
	defer lock.Unlock()
	logger.Debug("Restart xray, force:", isForce)

	xrayConfig, err := s.GetXrayConfig()
	if err != nil {
		return err
	}

	if len(p) > 0 && p[0].IsRunning() {
		if !isForce && p[0].GetConfig().Equals(xrayConfig) {
			logger.Debug("No need to restart xray")
			return nil
		}
		for i := range p {
			p[i].Stop()
		}
	}

	result = ""
	masterEnabled, err := s.settingService.GetMasterEnabled()
	if err != nil {
		return err
	}

	logger.Error("Master panel enabled:", masterEnabled)

	if masterEnabled {

		slaveIps, err := s.settingService.GetSlaveIps()
		if err != nil {
			return err
		}
		slaveRootPassword, err := s.settingService.GetSlaveRootPass()
		if err != nil {
			return err
		}

		p = p[:0]
		for i, host := range slaveIps {
			p = append(p, xray.NewRemoteProcess(xrayConfig, host, slaveRootPassword))
			res := p[i].Start()
			if err == nil {
				err = res
			}
		}
		return err
	}

	p = []xray.Process{xray.NewLocalProcess(xrayConfig)}
	return p[0].Start()
}

func (s *XrayService) StopXray() error {
	lock.Lock()
	defer lock.Unlock()
	logger.Debug("stop xray")

	err := error(nil)
	if s.IsXrayRunning() {
		for i := range p {
			res := p[i].Stop()
			if err == nil {
				err = res
			}
		}
		return err
	}
	return errors.New("xray is not running")
}

func (s *XrayService) SetToNeedRestart() {
	isNeedXrayRestart.Store(true)
}

func (s *XrayService) IsNeedRestartAndSetFalse() bool {
	return isNeedXrayRestart.CAS(true, false)
}
