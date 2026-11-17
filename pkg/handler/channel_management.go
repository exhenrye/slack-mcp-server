package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/slack-go/slack"
	"go.uber.org/zap"
)

// Channel management (conversations.create, conversations.invite) for the fork.
// Disabled by default; set SLACK_MCP_CHANNEL_MANAGEMENT_TOOL=true or 1 to enable.

const channelManagementEnv = "SLACK_MCP_CHANNEL_MANAGEMENT_TOOL"

var channelNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

type channelManagementResult struct {
	OK        bool     `json:"ok"`
	ChannelID string   `json:"channel_id"`
	Name      string   `json:"name"`
	IsPrivate bool     `json:"is_private"`
	Invited   []string `json:"invited,omitempty"`
}

func channelManagementEnabled() error {
	v := os.Getenv(channelManagementEnv)
	if v == "true" || v == "1" {
		return nil
	}
	return errors.New("channel management tools are disabled by default. Set " + channelManagementEnv +
		"=true (or 1) in the MCP server environment to enable conversations_create and conversations_invite")
}

// ConversationsCreateHandler creates a channel and optionally invites users to it.
func (ch *ConversationsHandler) ConversationsCreateHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := channelManagementEnabled(); err != nil {
		return nil, err
	}

	name := strings.TrimPrefix(strings.TrimSpace(request.GetString("name", "")), "#")
	if !channelNamePattern.MatchString(name) {
		return nil, fmt.Errorf("name %q is invalid: use lowercase letters, numbers, hyphens and underscores, max 80 characters", name)
	}
	isPrivate := request.GetBool("is_private", false)

	var userIDs []string
	if raw := request.GetString("users", ""); raw != "" {
		ids, err := ch.resolveUserIDs(raw)
		if err != nil {
			return nil, err
		}
		userIDs = ids
	}

	ch.logger.Debug("Creating Slack channel", zap.String("name", name), zap.Bool("private", isPrivate))
	channel, err := ch.apiProvider.Slack().CreateConversationContext(ctx, slack.CreateConversationParams{
		ChannelName: name,
		IsPrivate:   isPrivate,
	})
	if err != nil {
		ch.logger.Error("Slack CreateConversationContext failed", zap.Error(err))
		return nil, err
	}

	res := channelManagementResult{OK: true, ChannelID: channel.ID, Name: channel.Name, IsPrivate: isPrivate}
	if len(userIDs) > 0 {
		if _, err := ch.apiProvider.Slack().InviteUsersToConversationContext(ctx, channel.ID, userIDs...); err != nil {
			ch.logger.Error("Slack InviteUsersToConversationContext failed", zap.Error(err))
			return nil, fmt.Errorf("channel %s (%s) was created, but the invite failed: %w", channel.Name, channel.ID, err)
		}
		res.Invited = userIDs
	}
	return marshalChannelResult(res)
}

// ConversationsInviteHandler invites users to an existing channel.
func (ch *ConversationsHandler) ConversationsInviteHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := channelManagementEnabled(); err != nil {
		return nil, err
	}

	channel := strings.TrimSpace(request.GetString("channel_id", ""))
	if channel == "" {
		return nil, errors.New("channel_id is required")
	}
	if strings.HasPrefix(channel, "#") {
		maps := ch.apiProvider.ProvideChannelsMaps()
		id, ok := maps.ChannelsInv[channel]
		if !ok {
			return nil, fmt.Errorf("channel %q not found in cache; pass its ID instead", channel)
		}
		channel = maps.Channels[id].ID
	}

	userIDs, err := ch.resolveUserIDs(request.GetString("users", ""))
	if err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return nil, errors.New("users is required")
	}

	ch.logger.Debug("Inviting users", zap.String("channel", channel), zap.Strings("users", userIDs))
	result, err := ch.apiProvider.Slack().InviteUsersToConversationContext(ctx, channel, userIDs...)
	if err != nil {
		ch.logger.Error("Slack InviteUsersToConversationContext failed", zap.Error(err))
		return nil, err
	}
	return marshalChannelResult(channelManagementResult{
		OK: true, ChannelID: result.ID, Name: result.Name, IsPrivate: result.IsPrivate, Invited: userIDs,
	})
}

// resolveUserIDs turns a comma-separated list of user IDs, @handles, or exact
// real/display names into user IDs. Unknown or ambiguous names are an error,
// so nobody is invited by a guess.
func (ch *ConversationsHandler) resolveUserIDs(raw string) ([]string, error) {
	users := ch.apiProvider.ProvideUsersMap()
	var ids []string
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, err := resolveUserID(item, users.Users, users.UsersInv)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func resolveUserID(item string, users map[string]slack.User, usersInv map[string]string) (string, error) {
	if _, ok := users[item]; ok {
		return item, nil
	}
	handle := strings.ToLower(strings.TrimPrefix(item, "@"))
	if id, ok := usersInv[handle]; ok {
		return id, nil
	}
	var matches []string
	for id, u := range users {
		if u.Deleted || u.IsBot {
			continue
		}
		if strings.EqualFold(u.RealName, item) || strings.EqualFold(u.Profile.DisplayName, item) ||
			strings.EqualFold(u.Profile.RealName, item) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("user %q not found; pass a user ID, @handle or exact full name", item)
	default:
		return "", fmt.Errorf("user %q is ambiguous (%s); pass a user ID", item, strings.Join(matches, ", "))
	}
}

func marshalChannelResult(res channelManagementResult) (*mcp.CallToolResult, error) {
	out, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(out)), nil
}
