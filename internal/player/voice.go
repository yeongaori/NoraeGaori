package player

import (
	"context"
	"fmt"
	"time"

	"noraegaori/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type voiceConnection interface {
	OpusSendChan() chan []byte
	DeadChan() <-chan struct{}
	Err() error
	Speaking(bool) error
	Disconnect(context.Context) error
}

type discordVoice struct {
	vc *discordgo.VoiceConnection
}

func wrapVoiceConn(vc *discordgo.VoiceConnection) voiceConnection {
	if vc == nil {
		return nil
	}
	return &discordVoice{vc: vc}
}

func (d *discordVoice) OpusSendChan() chan []byte            { return d.vc.OpusSend }
func (d *discordVoice) DeadChan() <-chan struct{}            { return d.vc.Dead }
func (d *discordVoice) Err() error                           { return d.vc.Err }
func (d *discordVoice) Speaking(b bool) error                { return d.vc.Speaking(b) }
func (d *discordVoice) Disconnect(ctx context.Context) error { return d.vc.Disconnect(ctx) }

func (player *GuildPlayer) currentVoice() voiceConnection {
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.VoiceConn
}

func (player *GuildPlayer) setVoice(conn voiceConnection, channelID string) {
	player.mu.Lock()
	defer player.mu.Unlock()
	player.VoiceConn = conn
	player.VoiceChannelID = channelID
}

func JoinVoice(session *discordgo.Session, guildID, channelID string) (voiceConnection, error) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.VoiceConn != nil && player.VoiceChannelID == channelID {
		return player.VoiceConn, nil
	}

	session.RLock()
	existingVC, exists := session.VoiceConnections[guildID]
	session.RUnlock()
	if exists && existingVC != nil {
		logger.Infof("Found stale session voice connection, disconnecting for guild: %s", guildID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		existingVC.Disconnect(ctx)
		cancel()
	}

	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		player.VoiceConn.Disconnect(ctx)
		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vc, err := session.ChannelVoiceJoin(ctx, guildID, channelID, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if vc.Status == discordgo.VoiceConnectionStatusReady {
				logger.Debugf("Voice connection ready for guild: %s", guildID)
				player.VoiceConn = wrapVoiceConn(vc)
				player.VoiceChannelID = channelID
				return player.VoiceConn, nil
			}
		case <-vc.Dead:
			return nil, fmt.Errorf("voice connection died: %v", vc.Err)
		case <-timeout:
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			vc.Disconnect(ctx2)
			cancel2()
			return nil, fmt.Errorf("timeout waiting for voice")
		}
	}
}

func LeaveVoice(guildID string) error {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.VoiceConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := player.VoiceConn.Disconnect(ctx); err != nil {
			return fmt.Errorf("failed to disconnect: %w", err)
		}
		player.VoiceConn = nil
		player.VoiceChannelID = ""
		logger.Debugf("Left voice channel in guild: %s", guildID)
	}

	return nil
}
