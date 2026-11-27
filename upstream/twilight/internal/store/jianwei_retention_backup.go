package store

import (
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
	"strconv"
	"strings"
)

// Import never trusts cached grants, queued previews or live sampling cursors.
// Grant facts and the single wallet survive, while transient credentials do not.
func ValidateRetentionBackup(state *State) error {
	r := &state.Retention
	if r.Policy.Revision == 0 && len(r.Members) == 0 && len(r.Settlements) == 0 && len(r.Grants) == 0 && len(r.Watches) == 0 && len(r.ViewingWatches) == 0 && len(r.LastWatchAt) == 0 {
		return nil
	}
	if r.Policy.Validate() != nil || len(r.Members) > 100000 || len(r.Settlements) > 1000000 || len(r.Grants) > 1000000 {
		return fmt.Errorf("invalid retention policy or backup size")
	}
	for uid, v := range r.Members {
		u, ok := state.Users[uid]
		if !ok || v.Member.UID != uid || v.Member.Version < 1 || v.Policy.Validate() != nil || v.Member.PlaybackEnd != u.ExpiredAt || v.Member.ExemptEnd > v.Member.PlaybackEnd || v.Member.NextSwitchAt < 0 {
			return fmt.Errorf("invalid retention member backup")
		}
		if v.Member.Mode != "" && v.Member.Mode != retention.ModeA && v.Member.Mode != retention.ModeB {
			return fmt.Errorf("invalid retention method backup")
		}
		if window := v.Member.SelectedWindow; window != "" {
			owner, end, valid := strings.Cut(window, ":")
			windowUID, e1 := strconv.ParseInt(owner, 10, 64)
			deadline, e2 := strconv.ParseInt(end, 10, 64)
			if !valid || len(window) > 128 || e1 != nil || e2 != nil || windowUID != uid || deadline <= 0 || deadline > v.Member.PlaybackEnd {
				return fmt.Errorf("invalid retention selection window")
			}
		}
		if v.Member.Consent && (v.Member.Mode != retention.ModeA || !v.Member.Participation || v.Member.ConsentCap < v.Policy.Cost) {
			return fmt.Errorf("invalid retention authorization backup")
		}
		v.AppliedVersion = 0
		v.NotifiedVersion = 0
		r.Members[uid] = v
	}
	for id, v := range r.Settlements {
		if id != v.WindowID || v.UID <= 0 || v.NewEnd <= v.OldEnd || v.Cost < 0 || v.BalanceBefore-v.Cost != v.BalanceAfter || v.BalanceAfter < 0 {
			return fmt.Errorf("invalid retention ledger backup")
		}
		if _, ok := state.Users[v.UID]; !ok {
			return fmt.Errorf("unknown retention ledger member")
		}
	}
	for id, v := range r.Grants {
		if id != v.ID || v.UID <= 0 || v.At < 0 {
			return fmt.Errorf("invalid retention grant backup")
		}
	}
	for uid, items := range r.Watches {
		if _, ok := state.Users[uid]; !ok || len(items) > 10000 {
			return fmt.Errorf("invalid retention viewing backup")
		}
		for _, v := range items {
			if v.Start < 0 || v.End <= v.Start || v.End-v.Start > 37*retention.Day {
				return fmt.Errorf("invalid retention viewing interval")
			}
		}
	}
	for uid, last := range r.LastWatchAt {
		if _, ok := state.Users[uid]; !ok || last <= 0 {
			return fmt.Errorf("invalid last verified viewing timestamp")
		}
	}
	for uid, items := range r.ViewingWatches {
		if _, ok := state.Users[uid]; !ok || len(items) > 10000 {
			return fmt.Errorf("invalid verified viewing history")
		}
		for _, item := range items {
			if item.Start < 0 || item.End <= item.Start || item.End-item.Start > 37*retention.Day {
				return fmt.Errorf("invalid verified viewing history interval")
			}
		}
	}
	r.Quotes = map[string]RetentionQuote{}
	r.Samples = map[string]RetentionSample{}
	r.LastPoll = 0
	r.Quality = "restored_waiting_for_samples"
	return nil
}
