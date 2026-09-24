//go:build faults

package faultcheck

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands"
	"noraegaori/internal/database"
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/guild"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/configtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/localetest"
	"noraegaori/tests/testutil/queuetest"
)

const (
	runTimeout        = 5 * time.Second
	maxFailedRequests = 10
	crawlDepth        = 2
	songCount         = 12
	healthyScenario   = "healthy"
	faultStep         = "fault check"
	originMessageID   = "origin"
	reportFileName    = "faults-report.out"
)

type scenario struct {
	name   string
	status int
	around func(t *testing.T, run func())
}

type trigger func(fixture *commandtest.Fixture) error

type checker struct {
	t             *testing.T
	report        *Report
	seen          map[string]struct{}
	commandRuns   int
	componentRuns int
}

var (
	reportPath string
	scenarios  = []scenario{
		{name: healthyScenario, status: http.StatusOK, around: runDirectly},
		{name: "discord rejects", status: http.StatusBadRequest, around: runDirectly},
		{name: "discord fails", status: http.StatusInternalServerError, around: runDirectly},
		{name: "database closed", status: http.StatusOK, around: whileClosed},
		{name: "database query-only", status: http.StatusOK, around: whileQueryOnly},
	}
)

func TestMain(m *testing.M) {
	reportPath = os.Getenv("FAULTS_REPORT")
	if reportPath == "" {
		if workingDir, err := os.Getwd(); err == nil {
			reportPath = filepath.Join(workingDir, reportFileName)
		}
	}
	localetest.Run(m)
}

func TestFaults(t *testing.T) {
	configtest.Setup(t)
	commands.InitializeCommands()

	check := &checker{t: t, report: &Report{}, seen: make(map[string]struct{})}
	for _, cmd := range Commands() {
		for _, arguments := range Arguments(cmd) {
			check.command(cmd, arguments)
		}
	}

	check.writeReport()
	if check.report.HasFailures() {
		t.Errorf("the fault check found problems; see %s", reportPath)
	}
}

func runDirectly(_ *testing.T, run func()) {
	run()
}

func whileClosed(t *testing.T, run func()) {
	t.Helper()

	dbtest.WhileClosed(t, func() {
		invalidateCaches()
		run()
	})
}

func whileQueryOnly(t *testing.T, run func()) {
	t.Helper()

	dbtest.WhileClosed(t, func() {
		queryOnly, err := sql.Open("sqlite3", "file:"+filepath.Join("data", "database.sqlite")+"?_busy_timeout=5000&_query_only=true")
		if err != nil {
			t.Fatalf("failed to open the database in query-only mode: %v", err)
		}
		database.DB = queryOnly
		defer func() {
			if err := queryOnly.Close(); err != nil {
				t.Errorf("failed to close the query-only database: %v", err)
			}
		}()
		invalidateCaches()
		run()
	})
}

func invalidateCaches() {
	queue.InvalidateCache(commandtest.GuildID)
	guild.InvalidateCaches(commandtest.GuildID)
}

func newFixture(t *testing.T, status int) *commandtest.Fixture {
	t.Helper()

	owners := OwnerRotation(songCount, commandtest.CallerID, commandtest.OtherID)
	seeded := queuetest.Seed(t, commandtest.GuildID, queuetest.Songs(owners...)...)
	session, requests := discordtest.StubAPI(t, discordtest.Status(status))
	discordtest.AddGuild(t, session, commandtest.GuildID, discordtest.VoiceState(commandtest.GuildID, commandtest.CallerID, commandtest.VoiceChannelID, false))

	return &commandtest.Fixture{Session: session, Requests: requests, Queue: seeded}
}

func modalInteraction(customID string, member *discordgo.Member, components ...discordgo.MessageComponent) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        discordtest.InteractionID,
			AppID:     discordtest.InteractionAppID,
			Token:     discordtest.InteractionToken,
			Type:      discordgo.InteractionModalSubmit,
			GuildID:   commandtest.GuildID,
			ChannelID: discordtest.ChannelID,
			Member:    member,
			Message:   &discordgo.Message{ID: discordtest.PanelMessageID, ChannelID: discordtest.ChannelID},
			Data:      discordgo.ModalSubmitInteractionData{CustomID: customID, Components: components},
		},
	}
}

func (check *checker) command(cmd *command.Command, arguments []string) {
	label := strings.TrimSpace(cmd.Name + " " + strings.Join(arguments, " "))
	for _, isSlash := range []bool{true, false} {
		pathLabel := "text " + label
		if isSlash {
			pathLabel = "slash " + label
		}
		check.commandRuns++
		check.crawl(pathLabel, check.run(pathLabel, commandTrigger(cmd, arguments, isSlash)), crawlDepth)
	}
}

func (check *checker) crawl(parent string, payloads []map[string]any, depth int) {
	if depth == 0 {
		return
	}
	for _, payload := range payloads {
		for _, target := range Targets(payload) {
			key := targetKey(&target)
			if _, isSeen := check.seen[key]; isSeen {
				continue
			}
			check.seen[key] = struct{}{}

			label := parent + " > " + key
			check.componentRuns++
			check.crawl(label, check.run(label, componentTrigger(target)), depth-1)
		}
	}
}

func (check *checker) run(label string, fire trigger) []map[string]any {
	var healthyPayloads []map[string]any

	for index := range scenarios {
		current := &scenarios[index]
		check.t.Run(current.name, func(t *testing.T) {
			fixture := newFixture(t, current.status)

			var outcome Outcome
			current.around(t, func() {
				outcome = Guard(runTimeout, func() error { return fire(fixture) })
			})

			sent := fixture.Requests()
			if problem := describeProblem(&outcome, sent, current.status); problem != "" {
				check.report.Fail(Failure{Step: faultStep, Trigger: label, Scenario: current.name, Detail: problem})
			} else {
				check.report.Pass()
			}
			if current.name == healthyScenario {
				healthyPayloads = requestPayloads(sent)
			}
		})
	}
	return healthyPayloads
}

func (check *checker) writeReport() {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("%d command runs and %d button, menu and form runs, each under %d scenarios\n", check.commandRuns, check.componentRuns, len(scenarios)))
	if err := check.report.Print(&output); err != nil {
		check.t.Errorf("failed to print the report: %v", err)
	}
	if err := os.WriteFile(reportPath, []byte(output.String()), 0o644); err != nil {
		check.t.Errorf("failed to write the report: %v", err)
	}
	check.t.Log("\n" + output.String())
}

func describeProblem(outcome *Outcome, sent []discordtest.Request, status int) string {
	if crash := outcome.Crash(); crash != "" {
		return crash
	}
	if len(sent) == 0 && outcome.Err == nil {
		return "sent nothing to Discord and returned no error"
	}
	if status >= http.StatusBadRequest && len(sent) > maxFailedRequests {
		return fmt.Sprintf("sent %d requests to a failing Discord", len(sent))
	}
	return ""
}

func requestPayloads(sent []discordtest.Request) []map[string]any {
	payloads := make([]map[string]any, 0, len(sent))
	for index := range sent {
		if sent[index].Body != nil {
			payloads = append(payloads, sent[index].Body)
		}
	}
	return payloads
}

func commandTrigger(cmd *command.Command, arguments []string, isSlash bool) trigger {
	return func(fixture *commandtest.Fixture) error {
		origin := &discordgo.MessageCreate{Message: &discordgo.Message{ID: originMessageID, GuildID: commandtest.GuildID, ChannelID: discordtest.ChannelID}}
		ic := discord.CreatePseudoInteraction(origin, discordtest.Member(commandtest.GuildID, commandtest.CallerID), cmd.Name, cmd.Options, arguments)

		if isSlash {
			ic.ID, ic.AppID, ic.Token = discordtest.InteractionID, discordtest.InteractionAppID, discordtest.InteractionToken
			return cmd.Handler(fixture.Session, ic)
		}

		defer discord.RegisterResponder(ic.Token, &discord.MessageResponse{Session: fixture.Session, ChannelID: discordtest.ChannelID, OriginalMsgID: originMessageID})()
		return cmd.Handler(fixture.Session, ic)
	}
}

func componentTrigger(target Target) trigger {
	return func(fixture *commandtest.Fixture) error {
		member := discordtest.Member(commandtest.GuildID, commandtest.CallerID)
		if target.IsModal {
			command.HandleInteraction(fixture.Session, modalInteraction(target.CustomID, member, modalComponents(target.Fields)...))
			return nil
		}
		command.HandleInteraction(fixture.Session, discordtest.ComponentInteraction(commandtest.GuildID, target.CustomID, member, firstValue(target.Values)...))
		return nil
	}
}

func modalComponents(fields []Field) []discordgo.MessageComponent {
	components := make([]discordgo.MessageComponent, 0, len(fields))
	for index := range fields {
		field := &fields[index]
		if field.IsSelect {
			components = append(components, &discordgo.Label{Component: &discordgo.SelectMenu{CustomID: field.CustomID, Values: []string{field.Value}}})
			continue
		}
		components = append(components, &discordgo.ActionsRow{Components: []discordgo.MessageComponent{&discordgo.TextInput{CustomID: field.CustomID, Value: field.Value}}})
	}
	return components
}

func firstValue(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values[:1]
}

func targetKey(target *Target) string {
	if values := firstValue(target.Values); len(values) > 0 {
		return target.CustomID + "=" + values[0]
	}
	return target.CustomID
}
