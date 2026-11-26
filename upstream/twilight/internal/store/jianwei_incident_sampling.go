package store

import (
	"fmt"
	"github.com/prejudice-studio/twilight/internal/retention"
)

// Only a known server-side sampling outage may create an automatic incident.
// It protects access without inventing viewing seconds or crediting money.
func (s *Store) ProtectSamplingIncident(now int64) error {
	s.mu.RLock()
	last := s.state.Retention.LastPoll
	revision := s.state.Finance.Revision
	uids := []int64{}
	id := fmt.Sprintf("sampling-gap:%d", last)
	_, exists := s.state.Finance.Incidents[id]
	if s.state.Retention.Policy.Enabled && last > 0 && now-last > 90 && !exists {
		for _, uid := range s.userUIDs {
			u := s.state.Users[uid]
			r := s.state.Retention.Members[uid]
			if u.Role == RoleNormal && u.Active && u.EmbyID != "" && (!u.EmbyDisabled || u.EmbyAutoDisabled) && r.Member.Participation && r.Member.Mode == retention.ModeB && len(uids) < 500 {
				uids = append(uids, uid)
			}
		}
	}
	s.mu.RUnlock()
	if len(uids) == 0 {
		return nil
	}
	return s.CreateIncident(ServiceIncident{ID: id, Title: "观影采样中断保护", Reason: "服务器连续采样中断超过90秒；未伪造观影。请管理员核实故障区间后补偿或关闭。", Status: "open", UIDs: uids, Start: last, End: now, HoldUntil: now + 3*86400, Days: 0, Points: 0, Actor: 0}, revision)
}
