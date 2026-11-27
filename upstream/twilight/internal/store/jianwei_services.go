package store

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Service rights apply to additional libraries on the same Emby instance. They
// never change the main membership expiry or turn an ordinary user into white.
type MemberService struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	LibraryIDs      []string `json:"library_ids"`
	Lines           []string `json:"lines"`
	RegisterEnabled bool     `json:"register_enabled"`
	RenewEnabled    bool     `json:"renew_enabled"`
	Days            int      `json:"days"`
	PointsEnabled   bool     `json:"points_enabled"`
	Points          int      `json:"points"`
}
type ServiceState struct {
	Revision        int64                            `json:"revision"`
	Items           []MemberService                  `json:"items"`
	Grants          map[int64]map[string]int64       `json:"grants,omitempty"`
	LibraryBaseline map[int64]ServiceLibraryBaseline `json:"library_baseline,omitempty"`
}
type ServiceLibraryBaseline struct {
	All bool     `json:"all"`
	IDs []string `json:"ids"`
}

func serviceIDValid(id string) bool {
	return id == "music" || id == "audiobooks" || id == "white-cinema"
}
func defaultMemberServices() []MemberService {
	return []MemberService{{ID: "music", Name: "见微音乐", Days: 30}, {ID: "audiobooks", Name: "见微有声小说", Days: 30}, {ID: "white-cinema", Name: "见微白名单影视", Days: 30}}
}
func (s *Store) Services() ServiceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.state.Services
	v.Items = append([]MemberService{}, v.Items...)
	if len(v.Items) == 0 {
		v.Items = defaultMemberServices()
	}
	for i := range v.Items {
		v.Items[i].LibraryIDs = append([]string{}, v.Items[i].LibraryIDs...)
		v.Items[i].Lines = append([]string{}, v.Items[i].Lines...)
	}
	v.Grants = nil
	v.LibraryBaseline = nil
	return v
}
func (s *Store) ServiceGrants(uid int64) map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int64{}
	for id, end := range s.state.Services.Grants[uid] {
		out[id] = end
	}
	return out
}
func ServiceAccess(u User, v MemberService, until, now int64) bool {
	if !v.Enabled || !u.Active || u.EmbyID == "" || u.RebindingInProgress || u.EmbyDisabled && !u.EmbyAutoDisabled {
		return false
	}
	white := u.Role == RoleWhitelist && (u.WhitelistUntil == 0 || u.WhitelistUntil > now)
	return u.Role == RoleAdmin || white || until > now || until < 0
}
func validateMemberServices(items []MemberService) error {
	if len(items) != 3 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	libraries := map[string]bool{}
	hosts := map[string]bool{}
	for _, v := range items {
		if !serviceIDValid(v.ID) || seen[v.ID] || strings.TrimSpace(v.Name) == "" || len(v.Name) > 120 || len(v.LibraryIDs) > 64 || len(v.Lines) > 8 || v.Days < 1 || v.Days > 3650 || v.Points < 0 || v.Points > 10000000 {
			return ErrInvalid
		}
		seen[v.ID] = true
		if v.Enabled && len(v.LibraryIDs) == 0 {
			return fmt.Errorf("启用服务前请选择对应Emby媒体库")
		}
		if v.PointsEnabled && (!v.Enabled || v.Points < 1) {
			return fmt.Errorf("积分商品需要已启用服务和正整数价格")
		}
		for _, id := range v.LibraryIDs {
			if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\ \t\r\n") || libraries[id] {
				return fmt.Errorf("媒体库ID无效或重复归属")
			}
			libraries[id] = true
		}
		for _, raw := range v.Lines {
			u, e := url.Parse(raw)
			if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Port() != "" && u.Port() != "443" || hosts[strings.ToLower(u.Hostname())] {
				return fmt.Errorf("独立服务线路必须为不重复的HTTPS域名")
			}
			hosts[strings.ToLower(u.Hostname())] = true
		}
	}
	return nil
}

// Validation errors contain only fixed messages, safe for admin forms.
func ValidateMemberServices(items []MemberService) error { return validateMemberServices(items) }

func (s *Store) serviceShopEligibleLocked(u User, p ShopProduct, now int64) bool {
	v, exists := s.serviceLocked(p.ServiceID)
	if !exists || !v.Enabled || u.EmbyID == "" || !u.Active || u.RebindingInProgress || u.EmbyDisabled && !u.EmbyAutoDisabled || ServiceAccess(u, v, 0, now) {
		return false
	}
	end := s.state.Services.Grants[u.UID][v.ID]
	return p.Kind == 1 && v.RegisterEnabled && end <= now || p.Kind == 2 && v.RenewEnabled && end != 0
}

func (s *Store) SaveServices(items []MemberService, revision int64) error {
	if e := validateMemberServices(items); e != nil {
		return e
	}
	items = append([]MemberService{}, items...)
	for i := range items {
		items[i].LibraryIDs = append([]string{}, items[i].LibraryIDs...)
		items[i].Lines = append([]string{}, items[i].Lines...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		if s.state.Services.Revision != revision {
			return ErrConflict
		}
		s.state.Services.Items = items
		s.state.Services.Revision++
		return nil
	})
}
func (s *Store) serviceLocked(id string) (MemberService, bool) {
	for _, v := range s.state.Services.Items {
		if v.ID == id {
			return v, true
		}
	}
	return MemberService{}, false
}
func (s *Store) ConsumeServiceCode(uid, tgID int64, code string, now int64) (User, error) {
	u, _, e := s.ConsumeRegCodeAndUpdateUser(code, uid, tgID, func(u *User, r RegCode) error {
		v, ok := s.serviceLocked(r.ServiceID)
		if !ok || !v.Enabled || !u.Active || u.EmbyID == "" || u.RebindingInProgress || u.EmbyDisabled && !u.EmbyAutoDisabled || r.Days < 1 || r.Days > 3650 {
			return ErrEmbyRequired
		}
		if r.Type != 1 && r.Type != 2 {
			return ErrInvalid
		}
		end := s.state.Services.Grants[uid][v.ID]
		if ServiceAccess(*u, v, 0, now) {
			return fmt.Errorf("管理员或有效白名单已包含此服务，无需重复兑换")
		}
		if r.Type == 1 && end > now {
			return fmt.Errorf("此服务已开通，请使用对应续期码")
		}
		if r.Type == 2 && end == 0 {
			return fmt.Errorf("请先使用此服务的注册码")
		}
		if s.state.Services.Grants == nil {
			s.state.Services.Grants = map[int64]map[string]int64{}
		}
		if s.state.Services.Grants[uid] == nil {
			s.state.Services.Grants[uid] = map[string]int64{}
		}
		next := max(end, now) + int64(r.Days)*86400
		if next > now+30*365*86400 {
			return ErrInvalid
		}
		s.state.Services.Grants[uid][v.ID] = next
		s.state.Finance.ensure()
		s.state.Finance.Revision++
		sourceID := "code:" + code
		s.state.Finance.Sources[sourceID] = EntitlementSource{ID: sourceID, UID: uid, ServiceID: v.ID, Before: end, After: next, At: now, Sequence: s.state.Finance.Revision, Kind: "service_code"}
		return nil
	})
	return u, e
}
func (s *Store) ServicePointsProduct(u User, sku string, now int64) (ShopProduct, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servicePointsProductLocked(u, sku, now)
}
func (s *Store) servicePointsProductLocked(u User, sku string, now int64) (ShopProduct, int, error) {
	id := strings.TrimPrefix(sku, "svc-")
	v, ok := s.serviceLocked(id)
	if !ok || sku == id || !v.Enabled || !v.PointsEnabled || v.Points <= 0 || !u.Active || u.EmbyID == "" || u.RebindingInProgress || u.EmbyDisabled && !u.EmbyAutoDisabled || ServiceAccess(u, v, 0, now) {
		return ShopProduct{}, 0, ErrInvalid
	}
	kind := 1
	if s.state.Services.Grants[u.UID][id] != 0 {
		kind = 2
	}
	return ShopProduct{ID: "points-" + sku, Name: v.Name + "权益卡", ServiceID: id, Kind: kind, Days: v.Days, Stock: -1, Enabled: true}, v.Points, nil
}
func (s *Store) HasServiceAccess(u User, now int64) bool {
	cfg := s.Services()
	grants := s.ServiceGrants(u.UID)
	for _, v := range cfg.Items {
		if ServiceAccess(u, v, grants[v.ID], now) {
			return true
		}
	}
	return false
}

// Service configuration is finite and versioned; import rejects malformed or
// unexpectedly large grants rather than silently expanding library access.
func validateServiceState(v ServiceState) error {
	if v.Revision < 0 || len(v.Grants) > 100000 {
		return ErrInvalid
	}
	if len(v.Items) > 0 {
		if e := validateMemberServices(v.Items); e != nil {
			return e
		}
	}
	for uid, grants := range v.Grants {
		if uid <= 0 || len(grants) > 3 {
			return ErrInvalid
		}
		for id, end := range grants {
			if !serviceIDValid(id) || end < 0 || end > time.Now().AddDate(30, 0, 0).Unix() {
				return ErrInvalid
			}
		}
	}
	if len(v.LibraryBaseline) > 100000 {
		return ErrInvalid
	}
	for uid, baseline := range v.LibraryBaseline {
		if uid <= 0 || len(baseline.IDs) > 512 {
			return ErrInvalid
		}
		for _, id := range baseline.IDs {
			if id == "" || len(id) > 128 {
				return ErrInvalid
			}
		}
	}
	return nil
}

func (s *Store) ServiceBaseline(uid int64) (ServiceLibraryBaseline, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.state.Services.LibraryBaseline[uid]
	v.IDs = append([]string{}, v.IDs...)
	return v, ok
}
func (s *Store) RememberServiceBaseline(uid int64, all bool, ids []string) error {
	ids = append([]string{}, ids...)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		if _, ok := s.state.Users[uid]; !ok {
			return ErrNotFound
		}
		if _, ok := s.state.Services.LibraryBaseline[uid]; ok {
			return errNoChange
		}
		if len(ids) > 512 {
			return ErrInvalid
		}
		if s.state.Services.LibraryBaseline == nil {
			s.state.Services.LibraryBaseline = map[int64]ServiceLibraryBaseline{}
		}
		s.state.Services.LibraryBaseline[uid] = ServiceLibraryBaseline{All: all, IDs: ids}
		return nil
	})
}

func (s *Store) refundServiceSourceLocked(o *ShopOrder, source EntitlementSource, now int64) error {
	end := s.state.Services.Grants[o.UID][source.ServiceID]
	latest := source
	for _, v := range s.state.Finance.Sources {
		if v.UID == o.UID && v.ServiceID == source.ServiceID && v.RevokedAt == 0 && v.Sequence > latest.Sequence {
			latest = v
		}
	}
	if end != latest.After {
		return ErrConflict
	}
	remaining := max(int64(0), source.After-source.Offset-max(max(source.Before-source.Offset, source.At), now))
	if remaining > int64(o.Product.Days)*86400 {
		return ErrConflict
	}
	for id, v := range s.state.Finance.Sources {
		if v.UID == o.UID && v.ServiceID == source.ServiceID && v.RevokedAt == 0 && v.Sequence > source.Sequence && v.Kind != "refund" {
			v.Offset += remaining
			s.state.Finance.Sources[id] = v
		}
	}
	s.state.Services.Grants[o.UID][source.ServiceID] = max(now, end-remaining)
	source.RevokedAt = now
	source.RefundSeconds = remaining
	s.state.Finance.Sources[source.ID] = source
	s.state.Finance.Revision++
	refundID := "refund:" + o.ID
	s.state.Finance.Sources[refundID] = EntitlementSource{ID: refundID, UID: o.UID, ServiceID: source.ServiceID, Before: end, After: s.state.Services.Grants[o.UID][source.ServiceID], At: now, Sequence: s.state.Finance.Revision, Kind: "refund"}
	return nil
}
