package main

import (
	"fmt"
	"time"

	"github.com/dreamscached/minequery/v2"
)

type MinecraftChecker struct {
	serverAddress string
}

func NewMinecraftChecker(address string) *MinecraftChecker {
	return &MinecraftChecker{
		serverAddress: address,
	}
}

func (mc *MinecraftChecker) GetServerStatus() (*minequery.Status17, error) {
	pinger := minequery.NewPinger(
		minequery.WithTimeout(10 * time.Second),
	)

	status, err := pinger.Ping17(mc.serverAddress, 25565)
	if err != nil {
		return nil, err
	}

	return status, nil
}

func (mc *MinecraftChecker) IsServerEmpty() (bool, int, error) {
	status, err := mc.GetServerStatus()
	if err != nil {
		return false, 0, err
	}

	onlinePlayers := status.OnlinePlayers
	return onlinePlayers == 0, onlinePlayers, nil
}

func (mc *MinecraftChecker) IsServerReachable() (bool, error) {
	_, err := mc.GetServerStatus()
	return err == nil, err
}

func (mc *MinecraftChecker) WaitForServer(maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			reachable, _ := mc.IsServerReachable()
			if reachable {
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("server nicht erreichbar nach %v", maxWait)
			}
		case <-time.After(maxWait):
			return fmt.Errorf("timeout beim Warten auf Server")
		}
	}
}
