package store

import "time"

func (s *Store) FinancialLiability(uid int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.financialLiabilityLocked(uid)
}
func (s *Store) financialLiabilityLocked(uid int64) bool {
	if s.state.Finance.Holds[uid] > 0 {
		return true
	}
	for _, o := range s.state.ShopOrders {
		if o.UID == uid && (o.ReceivedCents > 0 || o.PointsCost > 0) && (o.Status == "paid_hold" || o.Status == "refund_pending" || o.Status == "issued" && (o.RedeemedAt == 0 || s.state.Users[uid].PendingEmby && s.state.Users[uid].RegistrationCode == o.CodeKey)) {
			return true
		}
	}
	return false
}
func (s *Store) ShopRefundPreview(id string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, found := s.state.ShopOrders[id]
	u := s.state.Users[o.UID]
	out := map[string]any{"current_expiry": u.ExpiredAt, "proven": false, "remaining_seconds": int64(0), "hint": "历史来源不足或权益已被外部修改，请人工核对，不能直接从总时长扣除"}
	if !found {
		return out
	}
	if o.RedeemedAt == 0 {
		out["proven"] = true
		out["hint"] = "冻结本人未使用码，核实实际退款后登记"
		return out
	}
	sourceID := o.EntitlementSourceID
	if sourceID == "" {
		sourceID = "code:" + o.CodeKey
	}
	v, ok := s.state.Finance.Sources[sourceID]
	if !ok || v.RevokedAt > 0 || u.ExpiredAt < 0 {
		return out
	}
	if u.EmbyID == "" && u.PendingEmby && u.RegistrationCode == o.CodeKey {
		out["proven"] = true
		out["hint"] = "撤回此订单尚未开通的资格；保留核销历史"
		return out
	}
	latest := v
	for _, next := range s.state.Finance.Sources {
		if next.UID == o.UID && next.RevokedAt == 0 && next.Sequence > latest.Sequence {
			latest = next
		}
	}
	if latest.After != u.ExpiredAt {
		return out
	}
	remaining := max(int64(0), v.After-v.Offset-max(max(v.Before-v.Offset, v.At), time.Now().Unix()))
	if remaining > int64(o.Product.Days)*86400 {
		return out
	}
	out["proven"] = true
	out["remaining_seconds"] = remaining
	out["result_expiry"] = max(time.Now().Unix(), u.ExpiredAt-remaining)
	out["hint"] = "仅撤回本订单未消费部分；保留后来独立权益。真实退款由管理员另行核实"
	return out
}
