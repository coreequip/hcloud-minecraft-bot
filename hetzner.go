package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type HetznerManager struct {
	client *hcloud.Client
	config *Config
}

func NewHetznerManager(config *Config) *HetznerManager {
	return &HetznerManager{
		client: hcloud.NewClient(hcloud.WithToken(config.HetznerToken)),
		config: config,
	}
}

func (h *HetznerManager) FindLatestSnapshot() (*hcloud.Image, error) {
	ctx := context.Background()

	opts := hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
	}

	images, err := h.client.Image.AllWithOpts(ctx, opts)
	if err != nil {
		return nil, err
	}

	var mcSnapshots []*hcloud.Image
	for _, img := range images {
		if strings.HasPrefix(img.Description, "mcone-") {
			mcSnapshots = append(mcSnapshots, img)
		}
	}

	if len(mcSnapshots) == 0 {
		return nil, fmt.Errorf("no Minecraft snapshots found")
	}

	sort.Slice(mcSnapshots, func(i, j int) bool {
		return mcSnapshots[i].Created.After(mcSnapshots[j].Created)
	})

	return mcSnapshots[0], nil
}

func (h *HetznerManager) FindServerByName(name string) (*hcloud.Server, error) {
	ctx := context.Background()

	servers, err := h.client.Server.All(ctx)
	if err != nil {
		return nil, err
	}

	for _, server := range servers {
		if server.Name == name {
			return server, nil
		}
	}

	return nil, nil
}

func (h *HetznerManager) CreateServerFromSnapshot(snapshot *hcloud.Image) (*hcloud.Server, *hcloud.Action, error) {
	ctx := context.Background()

	location, _, err := h.client.Location.Get(ctx, h.config.ServerLocation)
	if err != nil {
		return nil, nil, err
	}

	serverType, _, err := h.client.ServerType.Get(ctx, "cax31")
	if err != nil {
		return nil, nil, err
	}

	// Get SSH key by name
	sshKey, _, err := h.client.SSHKey.GetByName(ctx, "minecraft")
	if err != nil {
		log.Printf("[HETZNER] Warning: Could not find SSH key 'minecraft': %v", err)
		// Continue without SSH key
	}

	opts := hcloud.ServerCreateOpts{
		Name:       ServerName,
		ServerType: serverType,
		Image:      snapshot,
		Location:   location,
		Labels: map[string]string{
			"managed-by": "telegram-bot",
			"purpose":    "minecraft",
		},
		StartAfterCreate: hcloud.Ptr(true),
	}

	// Add SSH key if found
	if sshKey != nil {
		opts.SSHKeys = []*hcloud.SSHKey{sshKey}
		log.Printf("[HETZNER] Adding SSH key 'minecraft' to server")
	}

	result, _, err := h.client.Server.Create(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	return result.Server, result.Action, nil
}

func (h *HetznerManager) WaitForServerRunning(server *hcloud.Server) (*hcloud.Server, error) {
	ctx := context.Background()

	for i := 0; i < 60; i++ {
		updatedServer, _, err := h.client.Server.GetByID(ctx, server.ID)
		if err != nil {
			return nil, err
		}

		log.Printf("[HETZNER] Server Status: %s", updatedServer.Status)
		if updatedServer.Status == hcloud.ServerStatusRunning {
			return updatedServer, nil
		}

		time.Sleep(5 * time.Second)
	}

	return nil, fmt.Errorf("server did not start within 5 minutes")
}

func (h *HetznerManager) ShutdownServer(server *hcloud.Server) error {
	ctx := context.Background()

	log.Printf("[HETZNER] Sending shutdown command to server %s (ID: %d)", server.Name, server.ID)
	action, _, err := h.client.Server.Shutdown(ctx, server)
	if err != nil {
		return err
	}

	// Wait for shutdown to complete (max 2 minutes)
	done := make(chan error, 1)
	go func() {
		done <- h.waitForAction(action)
	}()

	select {
	case err := <-done:
		if err != nil {
			return err
		}
		log.Printf("[HETZNER] Server shutdown completed")
		return nil
	case <-time.After(2 * time.Minute):
		log.Printf("[HETZNER] Server shutdown timeout after 2 minutes")
		return nil // Continue anyway
	}
}

func (h *HetznerManager) CreateSnapshot(server *hcloud.Server) (*hcloud.Image, error) {
	ctx := context.Background()

	snapshotName := fmt.Sprintf("mcone-%s", time.Now().Format("2006-01-02-150405"))

	opts := &hcloud.ServerCreateImageOpts{
		Type:        hcloud.ImageTypeSnapshot,
		Description: hcloud.Ptr(snapshotName),
	}

	action, _, err := h.client.Server.CreateImage(ctx, server, opts)
	if err != nil {
		return nil, err
	}

	if err := h.waitForAction(action.Action); err != nil {
		return nil, err
	}

	image, _, err := h.client.Image.GetByID(ctx, action.Image.ID)
	if err != nil {
		return nil, err
	}

	return image, nil
}

func (h *HetznerManager) DeleteServer(server *hcloud.Server) error {
	ctx := context.Background()

	log.Printf("[HETZNER] Deleting server %s (ID: %d)", server.Name, server.ID)
	result, _, err := h.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return err
	}

	if result.Action != nil {
		log.Printf("[HETZNER] Server deletion started, Action ID: %d", result.Action.ID)
		return h.waitForAction(result.Action)
	}

	return nil
}

func (h *HetznerManager) waitForAction(action *hcloud.Action) error {
	ctx := context.Background()

	for {
		updatedAction, _, err := h.client.Action.GetByID(ctx, action.ID)
		if err != nil {
			return err
		}

		log.Printf("[HETZNER] Action %d Status: %s (Progress: %d%%)",
			updatedAction.ID, updatedAction.Status, updatedAction.Progress)

		if updatedAction.Status == hcloud.ActionStatusSuccess {
			return nil
		}

		if updatedAction.Status == hcloud.ActionStatusError {
			return fmt.Errorf("action failed: %s", updatedAction.ErrorMessage)
		}

		time.Sleep(10 * time.Second)
	}
}

func (h *HetznerManager) GetActionProgress(actionID int64) (int, error) {
	ctx := context.Background()

	action, _, err := h.client.Action.GetByID(ctx, actionID)
	if err != nil {
		return 0, err
	}

	return action.Progress, nil
}

func (h *HetznerManager) DeleteOldSnapshots(keepNewest int) error {
	ctx := context.Background()

	opts := hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
	}

	images, err := h.client.Image.AllWithOpts(ctx, opts)
	if err != nil {
		return err
	}

	var mcSnapshots []*hcloud.Image
	for _, img := range images {
		if strings.HasPrefix(img.Description, "mcone-") {
			mcSnapshots = append(mcSnapshots, img)
		}
	}

	if len(mcSnapshots) <= keepNewest {
		log.Printf("[HETZNER] No old snapshots to delete (have %d, keep %d)", len(mcSnapshots), keepNewest)
		return nil
	}

	// Sort by creation date, newest first
	sort.Slice(mcSnapshots, func(i, j int) bool {
		return mcSnapshots[i].Created.After(mcSnapshots[j].Created)
	})

	// Delete all snapshots except the newest ones
	for i := keepNewest; i < len(mcSnapshots); i++ {
		log.Printf("[HETZNER] Deleting old snapshot: %s (created: %s)", mcSnapshots[i].Description, mcSnapshots[i].Created)
		_, err := h.client.Image.Delete(ctx, mcSnapshots[i])
		if err != nil {
			log.Printf("[HETZNER] Error deleting snapshot %s: %v", mcSnapshots[i].Description, err)
			// Continue with other snapshots even if one fails
		}
	}

	log.Printf("[HETZNER] Old snapshots deleted. %d snapshots kept.", keepNewest)
	return nil
}
