package local

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"

	"common"
)

var (
	ErrDeniedPort        = errors.New("try to access denied port")
	ErrNotAllowedPort    = errors.New("try to access not allowed port")
	ErrDeniedIP          = errors.New("try to access denied IP")
	ErrNotAllowedIP      = errors.New("try to access not allowed IP")
	ErrLoopProxy         = errors.New("try to access proxy server")
	loadBalancePolicyMap = map[string]func(){
		"smart":      prepareSmartLoadBalance,
		"roundrobin": prepareRoundRobinLoadBalance,
		"none":       prepareNoneLoadBalance,
	}
)

func filterIPv4TCPOutbound(conn net.Conn, rawaddr []byte) error {
	targetIP := net.IPv4(rawaddr[1], rawaddr[2], rawaddr[3], rawaddr[4])
	port := int(binary.BigEndian.Uint16(rawaddr[5:]))
	ipAddr := binary.BigEndian.Uint32(rawaddr[1:5])
	if _, ok := deniedPort[port]; ok {
		common.Warning(conn.RemoteAddr(), "is trying to access denied port", port)
		return ErrDeniedPort
	}
	if config.Target.Port.Deny == "all" {
		if _, ok := allowedPort[port]; !ok {
			common.Warning(conn.RemoteAddr(), "is trying to access not allowed port", port)
			return ErrNotAllowedPort
		}
	}
	if _, ok := deniedIP[ipAddr]; ok {
		common.Warning(conn.RemoteAddr(), "is trying to access denied IP", targetIP)
		return ErrDeniedIP
	}
	if config.Target.IP.Deny == "all" {
		if _, ok := allowedIP[ipAddr]; !ok {
			common.Warning(conn.RemoteAddr(), "is trying to access not allowed IP", targetIP)
			return ErrNotAllowedIP
		}
	}
	if p, ok := serverIP[ipAddr]; ok && port == p {
		common.Warningf("%v is trying to access proxy server %v:%d",
			conn.RemoteAddr(), targetIP, port)
		backends.RLock()
		defer backends.RUnlock()
		for _, si := range backends.BackendsInformation {
			for _, ip := range si.ips {
				if ip.Equal(targetIP) {
					si.firewalled = true
					break // may be more than one IPs, so continue looping
				}
			}
		}
		return ErrLoopProxy
	}
	common.Debug("try to access:", targetIP, port)
	return nil
}

func prepareSmartLoadBalance() {
	outboundLoadBalanceHandler = smartLoadBalance
}

func prepareRoundRobinLoadBalance() {
	outboundLoadBalanceHandler = roundRobinLoadBalance
}

func prepareNoneLoadBalance() {
	outboundIndex = 0
	outboundLoadBalanceHandler = indexSpecifiedLoadBalance
}

func prepareDefaultLoadBalance() {
	if strings.Index(config.Generals.LoadBalance, "index:") == 0 {
		if index, err := strconv.Atoi(config.Generals.LoadBalance[6:]); err == nil {
			outboundIndex = index
			outboundLoadBalanceHandler = indexSpecifiedLoadBalance
		} else {
			common.Error("wrong index specified load balance method format, use smart method now")
			outboundLoadBalanceHandler = smartLoadBalance
		}
	}
}

func handleTCPOutbound(conn net.Conn, rawaddr []byte, _ string) error {
	defer conn.Close()
	switch rawaddr[0] {
	case 1:
		// IPv4
		if err := filterIPv4TCPOutbound(conn, rawaddr); err != nil {
			return err
		}
		//case 3:
		// variant length domain name
		//case 4:
		// IPv6
	}

	if outboundLoadBalanceHandler == nil {
		if prepareLoadBalance, ok := loadBalancePolicyMap[config.Generals.LoadBalance]; ok {
			prepareLoadBalance()
		} else {
			prepareDefaultLoadBalance()
		}
	}

	outboundLoadBalanceHandler(conn, rawaddr)
	return nil
}