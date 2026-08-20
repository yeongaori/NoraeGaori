package vote

import "sync"

type guildVotes struct {
	mu       sync.RWMutex
	sessions map[Kind]*Session
}

type voteRegistry struct {
	mu     sync.RWMutex
	guilds map[string]*guildVotes

	messageMu sync.RWMutex
	byMessage map[string]*Session
}

func newVoteRegistry() *voteRegistry {
	return &voteRegistry{
		guilds:    make(map[string]*guildVotes),
		byMessage: make(map[string]*Session),
	}
}

var activeVotes = newVoteRegistry()

func (r *voteRegistry) guildEntry(guildID string) *guildVotes {
	r.mu.RLock()
	entry := r.guilds[guildID]
	r.mu.RUnlock()

	if entry != nil {
		return entry
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if entry := r.guilds[guildID]; entry != nil {
		return entry
	}

	entry = &guildVotes{sessions: make(map[Kind]*Session)}
	r.guilds[guildID] = entry
	return entry
}

func (r *voteRegistry) snapshotOf(guildID string, kind Kind) (voteSnapshot, bool) {
	entry := r.guildEntry(guildID)

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	session := entry.sessions[kind]
	if session == nil {
		return voteSnapshot{}, false
	}
	return session.snapshot(), true
}

func (r *voteRegistry) claim(session *Session) (voteSnapshot, bool) {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if existing := entry.sessions[session.kind]; existing != nil {
		return existing.snapshot(), false
	}

	entry.sessions[session.kind] = session
	return voteSnapshot{}, true
}

func (r *voteRegistry) attachMessage(session *Session, messageID, channelID string) bool {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	if entry.sessions[session.kind] != session {
		entry.mu.Unlock()
		return false
	}
	session.messageID = messageID
	session.channelID = channelID
	entry.mu.Unlock()

	r.messageMu.Lock()
	r.byMessage[messageID] = session
	r.messageMu.Unlock()
	return true
}

func (r *voteRegistry) unindexMessage(messageID string, session *Session) {
	if messageID == "" {
		return
	}

	r.messageMu.Lock()
	defer r.messageMu.Unlock()

	if r.byMessage[messageID] == session {
		delete(r.byMessage, messageID)
	}
}

func (r *voteRegistry) sessionForMessage(messageID string) *Session {
	r.messageMu.RLock()
	defer r.messageMu.RUnlock()

	return r.byMessage[messageID]
}

func (r *voteRegistry) release(session *Session) {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	owned := entry.sessions[session.kind] == session
	if owned {
		delete(entry.sessions, session.kind)
	}
	messageID := session.messageID
	entry.mu.Unlock()

	if owned {
		r.unindexMessage(messageID, session)
	}
}

func (r *voteRegistry) resolve(session *Session) bool {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	if entry.sessions[session.kind] != session {
		entry.mu.Unlock()
		return false
	}
	session.resolved = true
	delete(entry.sessions, session.kind)
	messageID := session.messageID
	entry.mu.Unlock()

	r.unindexMessage(messageID, session)
	return true
}

func (r *voteRegistry) cancel(guildID string, reason voteEndReason, kinds ...Kind) {
	if len(kinds) == 0 {
		kinds = allVoteKinds
	}

	type endedVote struct {
		session   *Session
		messageID string
	}

	entry := r.guildEntry(guildID)
	ended := make([]endedVote, 0, len(kinds))

	entry.mu.Lock()
	for _, kind := range kinds {
		session := entry.sessions[kind]
		if session == nil {
			continue
		}
		delete(entry.sessions, kind)
		ended = append(ended, endedVote{session: session, messageID: session.messageID})
	}
	entry.mu.Unlock()

	for _, vote := range ended {
		r.unindexMessage(vote.messageID, vote.session)
		vote.session.endWith(reason)
	}
}

func (r *voteRegistry) recordVote(session *Session, ballot voteBallot, threshold voteThreshold) (Tally, bool) {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.sessions[session.kind] != session {
		return Tally{}, false
	}

	if !session.castVote(ballot) {
		return Tally{}, false
	}

	return session.tally(threshold), true
}

func (r *voteRegistry) retractVote(session *Session, ballot voteBallot, threshold voteThreshold) (Tally, bool) {
	entry := r.guildEntry(session.guildID)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.sessions[session.kind] != session {
		return Tally{}, false
	}

	if !session.withdrawVote(ballot) {
		return Tally{}, false
	}

	tally := session.tally(threshold)
	tally.passed = false
	tally.byAdderConsent = false
	return tally, true
}
