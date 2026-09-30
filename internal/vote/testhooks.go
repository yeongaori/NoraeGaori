//go:build testhooks

package vote

import "github.com/bwmarrin/discordgo"

const HookResolveFromCache = resolveFromCache
const HookVoteEndCancelled = voteEndCancelled
const HookVoteEndExpired = voteEndExpired
const HookVoteEndPassed = voteEndPassed
const HookVoteEndQueueEnded = voteEndQueueEnded
const HookVoteEndSuperseded = voteEndSuperseded

type HookMemberResolution = memberResolution
type HookVoteBallot = voteBallot
type HookVoteEndReason = voteEndReason
type HookVoteThreshold = voteThreshold

type HookTallyFields struct {
	Current        int
	Required       int
	AdderVotes     int
	AdderTotal     int
	Passed         bool
	ByAdderConsent bool
}

type HookVoteBallotFields struct {
	UserID    string
	CountsFor bool
	IsAdder   bool
}

type HookVoteThresholdFields struct {
	Quorum int
	Adders []string
}

var HookActiveVotes = &activeVotes
var HookAddersFor = &addersFor
var HookAwaitVoteOutcome = awaitVoteOutcome
var HookClassifyVoter = classifyVoter
var HookCurrentThreshold = currentThreshold
var HookEditVoteMessage = &editVoteMessage
var HookEndedBeforeAttach = endedBeforeAttach
var HookEveryAdderVoted = everyAdderVoted
var HookIsAdder = isAdder
var HookNewVoteRegistry = newVoteRegistry
var HookNewVoteSession = newVoteSession
var HookOnVoteReactionAdd = onVoteReactionAdd
var HookOnVoteReactionRemove = onVoteReactionRemove
var HookRenderVoteEnded = renderVoteEnded
var HookVoteEndDescription = voteEndDescription
var HookVoteExpirationTime = &voteExpirationTime
var HookVoteForReaction = voteForReaction
var HookVoteMessageURL = voteMessageURL
var HookVoteProgressEmbed = voteProgressEmbed

func HookBuildTally(fields HookTallyFields) *Tally {
	return &Tally{
		current:        fields.Current,
		required:       fields.Required,
		adderVotes:     fields.AdderVotes,
		adderTotal:     fields.AdderTotal,
		passed:         fields.Passed,
		byAdderConsent: fields.ByAdderConsent,
	}
}

func HookBuildVoteBallot(fields HookVoteBallotFields) *voteBallot {
	return &voteBallot{userID: fields.UserID, countsFor: fields.CountsFor, isAdder: fields.IsAdder}
}

func HookBuildVoteThreshold(fields HookVoteThresholdFields) *voteThreshold {
	return &voteThreshold{quorum: fields.Quorum, adders: fields.Adders}
}

func (vs *Session) HookAdderVotes() *map[string]struct{} {
	return &vs.adderVotes
}

func (vs *Session) HookChannelID() *string {
	return &vs.channelID
}

func (vs *Session) HookDone() *chan voteEndReason {
	return &vs.done
}

func (vs *Session) HookKind() *Kind {
	return &vs.kind
}

func (vs *Session) HookMessageID() *string {
	return &vs.messageID
}

func (vs *Session) HookOnPassed() *func(s *discordgo.Session, session *Session, tally Tally) {
	return &vs.onPassed
}

func (vs *Session) HookResolved() *bool {
	return &vs.resolved
}

func (vs *Session) HookVotes() *map[string]struct{} {
	return &vs.votes
}

func (vs *Session) HookCastVote(ballot voteBallot) bool {
	return vs.castVote(ballot)
}

func (vs *Session) HookEndWith(reason voteEndReason) {
	vs.endWith(reason)
}

func (vs *Session) HookTally(threshold voteThreshold) Tally {
	return vs.tally(threshold)
}

func (vs *Session) HookWithdrawVote(ballot voteBallot) bool {
	return vs.withdrawVote(ballot)
}

func (t *Tally) HookAdderTotal() *int {
	return &t.adderTotal
}

func (t *Tally) HookAdderVotes() *int {
	return &t.adderVotes
}

func (t *Tally) HookByAdderConsent() *bool {
	return &t.byAdderConsent
}

func (t *Tally) HookCurrent() *int {
	return &t.current
}

func (t *Tally) HookPassed() *bool {
	return &t.passed
}

func (t *Tally) HookRequired() *int {
	return &t.required
}

func (v *voteBallot) HookCountsFor() *bool {
	return &v.countsFor
}

func (v *voteBallot) HookIsAdder() *bool {
	return &v.isAdder
}

func (v *voteSnapshot) HookChannelID() *string {
	return &v.channelID
}

func (v *voteSnapshot) HookMessageID() *string {
	return &v.messageID
}

func (v *voteThreshold) HookQuorum() *int {
	return &v.quorum
}

func (r *voteRegistry) HookAttachMessage(session *Session, messageID string, channelID string) bool {
	return r.attachMessage(session, messageID, channelID)
}

func (r *voteRegistry) HookCancel(guildID string, reason voteEndReason, kinds ...Kind) {
	r.cancel(guildID, reason, kinds...)
}

func (r *voteRegistry) HookClaim(session *Session) (voteSnapshot, bool) {
	return r.claim(session)
}

func (r *voteRegistry) HookRecordVote(session *Session, ballot voteBallot, threshold voteThreshold) (Tally, bool) {
	return r.recordVote(session, ballot, threshold)
}

func (r *voteRegistry) HookRelease(session *Session) {
	r.release(session)
}

func (r *voteRegistry) HookResolve(session *Session) bool {
	return r.resolve(session)
}

func (r *voteRegistry) HookRetractVote(session *Session, ballot voteBallot, threshold voteThreshold) (Tally, bool) {
	return r.retractVote(session, ballot, threshold)
}

func (r *voteRegistry) HookSessionForMessage(messageID string) *Session {
	return r.sessionForMessage(messageID)
}

func (r *voteRegistry) HookSnapshotOf(guildID string, kind Kind) (voteSnapshot, bool) {
	return r.snapshotOf(guildID, kind)
}
