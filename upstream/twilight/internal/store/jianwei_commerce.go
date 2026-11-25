package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

type ShopProduct struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       int    `json:"kind"` // 1 registration, 2 renewal. Whitelist is not sold implicitly.
	Days       int    `json:"days"`
	PriceCents int64  `json:"price_cents"`
	Enabled    bool   `json:"enabled"`
	Stock      int    `json:"stock"` // -1 unlimited; otherwise includes pending reservations.
}
type ShopSettings struct {
	Revision         int64         `json:"revision"`
	Enabled          bool          `json:"enabled"`
	Instructions     string        `json:"instructions"`
	ReceivingAccount string        `json:"receiving_account"`
	Channel          string        `json:"channel"`
	Products         []ShopProduct `json:"products"`
}
type ShopOrder struct {
	ID               string      `json:"id"`
	UID              int64       `json:"uid"`
	Revision         int64       `json:"revision"`
	RequestKey       string      `json:"request_key"`
	Product          ShopProduct `json:"product"`
	Channel          string      `json:"channel"`
	ReceivingAccount string      `json:"receiving_account"`
	Instructions     string      `json:"instructions"`
	Status           string      `json:"status"`
	CreatedAt        int64       `json:"created_at"`
	UpdatedAt        int64       `json:"updated_at"`
	PayBefore        int64       `json:"pay_before"`
	CapacityReserved bool        `json:"capacity_reserved"`
	Note             string      `json:"note,omitempty"`
	AdminNote        string      `json:"admin_note,omitempty"`
	ReceiptHash      string      `json:"receipt_hash,omitempty"`
	ReceivedCents    int64       `json:"received_cents,omitempty"`
	PaidAt           int64       `json:"paid_at,omitempty"`
	CheckedBy        int64       `json:"checked_by,omitempty"`
	CodeKey          string      `json:"code_key,omitempty"`
	CodeCipher       string      `json:"code_cipher,omitempty"`
	RefundReference  string      `json:"refund_reference,omitempty"`
	RefundAt         int64       `json:"refund_at,omitempty"`
	RedeemedAt       int64       `json:"redeemed_at,omitempty"`
}

func (s *Store) ShopConfiguration() ShopSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.state.Shop
	v.Products = cloneSlice(v.Products)
	return v
}
func (s *Store) SaveShopConfiguration(settings ShopSettings, expected int64) error {
	if len(settings.Products) > 24 || len(settings.Instructions) > 4000 || len(settings.ReceivingAccount) > 100 || len(settings.Channel) > 60 {
		return ErrInvalid
	}
	ids := map[string]bool{}
	for _, p := range settings.Products {
		if p.ID == "" || len(p.ID) > 40 || ids[p.ID] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || p.Days < 1 || p.Days > 3650 || p.PriceCents < 1 || p.PriceCents > 10000000 || (p.Kind != 1 && p.Kind != 2) || p.Stock < -1 {
			return ErrInvalid
		}
		ids[p.ID] = true
	}
	if settings.Enabled && (strings.TrimSpace(settings.Instructions) == "" || strings.TrimSpace(settings.Channel) == "" || strings.TrimSpace(settings.ReceivingAccount) == "" || len(settings.Products) == 0) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateAndSaveLocked(func() error {
		if s.state.Shop.Revision != expected {
			return ErrConflict
		}
		settings.Revision = expected + 1
		settings.Products = cloneSlice(settings.Products)
		s.state.Shop = settings
		return nil
	})
}
func (s *Store) ShopOrders(uid int64, admin bool) []ShopOrder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []ShopOrder{}
	for _, o := range s.state.ShopOrders {
		if admin || o.UID == uid {
			items = append(items, o)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items
}
func (s *Store) ShopOrder(id string) (ShopOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.state.ShopOrders[id]
	return v, ok
}
func orderReserves(o ShopOrder, now int64) bool {
	return o.CapacityReserved && (o.Status == "review" || o.Status == "hold" || (o.Status == "pending" && o.PayBefore > now))
}
func (s *Store) CreateShopOrder(uid int64, id, productID, requestKey string, revision int64, limit int, countActive bool) (ShopOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	err := s.mutateAndSaveLocked(func() error {
		u, found := s.state.Users[uid]
		if !found || !u.Active || u.TelegramID <= 0 {
			return ErrNotFound
		}
		if len(id) < 16 || len(requestKey) < 16 || len(requestKey) > 80 {
			return ErrInvalid
		}
		for _, o := range s.state.ShopOrders {
			if o.UID == uid && o.RequestKey == requestKey {
				if o.Product.ID != productID {
					return ErrConflict
				}
				result = o
				return errNoChange
			}
		}
		cfg := s.state.Shop
		if !cfg.Enabled || cfg.Revision != revision {
			return ErrConflict
		}
		var product ShopProduct
		for _, p := range cfg.Products {
			if p.ID == productID && p.Enabled {
				product = p
			}
		}
		if product.ID == "" {
			return ErrNotFound
		}
		if product.Kind == 1 && (u.EmbyID != "" || u.PendingEmby || u.EmbyGrantLocked) {
			return ErrConflict
		}
		if product.Kind == 2 && u.EmbyID == "" {
			return ErrInvalid
		}
		now := time.Now().Unix()
		reserved, sold, pending := 0, 0, 0
		for _, o := range s.state.ShopOrders {
			active := orderReserves(o, now)
			if o.UID == uid && active {
				pending++
			}
			if o.Product.ID == productID {
				if active {
					reserved++
				}
				if o.CodeKey != "" && o.Status != "refunded" {
					sold++
				}
			}
		}
		if pending >= 2 || (product.Stock >= 0 && reserved+sold >= product.Stock) {
			return ErrConflict
		}
		if product.Kind == 1 && limit > 0 && s.embyOccupancyLocked(EmbyOccupancyOptions{CountActiveUsers: countActive, ExcludeUID: uid}) >= limit {
			return ErrEmbyCapacityReached
		}
		if _, exists := s.state.ShopOrders[id]; exists {
			return ErrConflict
		}
		result = ShopOrder{ID: id, UID: uid, Revision: 1, RequestKey: requestKey, Product: product, Channel: cfg.Channel, ReceivingAccount: cfg.ReceivingAccount, Instructions: cfg.Instructions, Status: "pending", CreatedAt: now, UpdatedAt: now, PayBefore: now + 900, CapacityReserved: true}
		s.state.ShopOrders[id] = result
		return nil
	})
	return result, err
}
func (s *Store) SubmitShopPayment(uid int64, id, note string, cancel bool) (ShopOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	err := s.mutateAndSaveLocked(func() error {
		o, found := s.state.ShopOrders[id]
		if !found || o.UID != uid {
			return ErrNotFound
		}
		if len(note) > 500 {
			return ErrInvalid
		}
		if !cancel && (o.Status == "review" || o.Status == "hold" || o.Status == "issued") {
			result = o
			return errNoChange
		}
		if o.Status != "pending" {
			return ErrConflict
		}
		if cancel {
			o.Status = "cancelled"
		} else if o.PayBefore <= time.Now().Unix() {
			o.Status = "hold"
			o.CapacityReserved = false
		} else {
			o.Status = "review"
		}
		o.Note = note
		o.Revision++
		o.UpdatedAt = time.Now().Unix()
		s.state.ShopOrders[id] = o
		result = o
		return nil
	})
	return result, err
}

// ConfirmShopPayment commits the verified receipt and the single-use code in
// one state CAS transaction. Repeated confirmations never mint another code.
func (s *Store) ConfirmShopPayment(id string, revision, admin, received, paidAt int64, receiptHash, codeKey, cipher, note string, limit int, countActive bool) (ShopOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	err := s.mutateAndSaveLocked(func() error {
		o, ok := s.state.ShopOrders[id]
		if !ok {
			return ErrNotFound
		}
		if o.Status == "issued" {
			if o.ReceiptHash != receiptHash || o.ReceivedCents != received {
				return ErrConflict
			}
			result = o
			return errNoChange
		}
		if o.Revision != revision || (o.Status != "review" && o.Status != "hold" && o.Status != "paid_hold") || received != o.Product.PriceCents || paidAt <= 0 || paidAt > time.Now().Unix()+300 || len(receiptHash) != 64 || len(codeKey) != 64 || cipher == "" || len(note) > 1000 {
			return ErrInvalid
		}
		if o.ReceiptHash != "" && (o.ReceiptHash != receiptHash || o.ReceivedCents != received) {
			return ErrConflict
		}
		for _, other := range s.state.ShopOrders {
			if other.ID != id && other.ReceiptHash == receiptHash {
				return ErrConflict
			}
		}
		if _, exists := s.state.RegCodes[codeKey]; exists {
			return ErrConflict
		}
		u, ok := s.state.Users[o.UID]
		o.ReceiptHash = receiptHash
		o.ReceivedCents = received
		o.PaidAt = paidAt
		o.CheckedBy = admin
		o.AdminNote = note
		ready := ok && u.Active && u.TelegramID > 0
		// Recheck stock after a late payment lost its reservation. A paid hold
		// records the real receipt without over-selling or silently dropping it.
		stock := o.Product.Stock
		for _, p := range s.state.Shop.Products {
			if p.ID == o.Product.ID {
				stock = p.Stock
			}
		}
		occupied := 0
		for _, other := range s.state.ShopOrders {
			if other.ID != id && other.Product.ID == o.Product.ID && (orderReserves(other, time.Now().Unix()) || (other.CodeKey != "" && other.Status != "refunded")) {
				occupied++
			}
		}
		if stock >= 0 && occupied >= stock {
			ready = false
		}
		if o.Product.Kind == 1 {
			ready = ready && u.EmbyID == "" && !u.PendingEmby && !u.EmbyGrantLocked
			if limit > 0 && s.embyOccupancyLocked(EmbyOccupancyOptions{CountActiveUsers: countActive, ExcludeShopOrder: id, ExcludeUID: o.UID}) >= limit {
				ready = false
			}
		} else {
			ready = ready && u.EmbyID != ""
		}
		if !ready {
			o.Status = "paid_hold"
			o.CapacityReserved = false
			o.Revision++
			o.UpdatedAt = time.Now().Unix()
			s.state.ShopOrders[id] = o
			result = o
			return nil
		}
		now := time.Now().Unix()
		rc := RegCode{Code: codeKey, Type: o.Product.Kind, Days: o.Product.Days, UseCountLimit: 1, Active: true, TargetUID: o.UID, TargetTelegramID: u.TelegramID, Source: "commerce", CreatorUID: admin, CreatedAt: now, CreatedTime: now, ValidityTime: 30 * 24, Note: "shop:" + id}
		s.state.RegCodes[codeKey] = rc
		o.Status = "issued"
		o.CodeKey = codeKey
		o.CodeCipher = cipher
		o.ReceiptHash = receiptHash
		o.ReceivedCents = received
		o.PaidAt = paidAt
		o.CheckedBy = admin
		o.AdminNote = note
		o.Revision++
		o.UpdatedAt = now
		s.state.ShopOrders[id] = o
		result = o
		return nil
	})
	return result, err
}

// Refund submission reserves the code against redemption. Completing a refund
// records a real external transfer reference; this method never sends money.
func (s *Store) ReviewShopOrder(id, action, note, refundReference string, revision, admin int64) (ShopOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ShopOrder
	err := s.mutateAndSaveLocked(func() error {
		o, found := s.state.ShopOrders[id]
		if !found {
			return ErrNotFound
		}
		if action == "refund_complete" && o.Status == "refunded" && o.RefundReference == refundReference {
			result = o
			return errNoChange
		}
		if o.Revision != revision || len(note) > 1000 {
			return ErrConflict
		}
		switch action {
		case "hold":
			if o.Status != "review" && o.Status != "hold" {
				return ErrConflict
			}
			o.Status = "hold"
		case "close_unpaid":
			if (o.Status != "hold" && o.Status != "review" && o.Status != "pending") || strings.TrimSpace(note) == "" {
				return ErrConflict
			}
			o.Status = "closed_unpaid"
		case "refund_pending":
			if o.RedeemedAt > 0 || (o.Status != "issued" && o.Status != "paid_hold") {
				return ErrConflict
			}
			if o.CodeKey != "" {
				rc, ok := s.state.RegCodes[o.CodeKey]
				if !ok || rc.UseCount > 0 {
					return ErrConflict
				}
				rc.Active = false
				s.state.RegCodes[o.CodeKey] = rc
			}
			o.Status = "refund_pending"
			o.CodeCipher = ""
		case "refund_complete":
			if o.Status != "refund_pending" || strings.TrimSpace(refundReference) == "" || len(refundReference) > 200 {
				return ErrConflict
			}
			for _, other := range s.state.ShopOrders {
				if other.ID != id && other.RefundReference == refundReference {
					return ErrConflict
				}
			}
			o.Status = "refunded"
			o.RefundReference = refundReference
			o.RefundAt = time.Now().Unix()
		case "extend_code":
			if o.RedeemedAt > 0 || o.Status != "issued" || strings.TrimSpace(note) == "" {
				return ErrConflict
			}
			rc, ok := s.state.RegCodes[o.CodeKey]
			if !ok || !rc.Active || rc.UseCount > 0 || !RegCodeExpired(rc, time.Now().Unix()) {
				return ErrConflict
			}
			rc.ValidityTime = (time.Now().Unix() + 30*86400 - rc.CreatedAt + 3599) / 3600
			s.state.RegCodes[o.CodeKey] = rc
		default:
			return ErrInvalid
		}
		o.AdminNote = note
		o.CheckedBy = admin
		o.Revision++
		o.UpdatedAt = time.Now().Unix()
		s.state.ShopOrders[id] = o
		result = o
		return nil
	})
	return result, err
}

func (s *Store) RevokeShopSession(ctx context.Context, token, botKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM twilight_mini_sessions WHERE token_hash=$1 AND bot_hash=$2`, identityDigest(token), identityDigest("shop-session|"+botKey))
	return err
}

// Reuse the ephemeral session table with domain-separated purpose keys. Import
// and restore already clear this table, invalidating login codes and sessions.
func (s *Store) CreateShopLoginCode(ctx context.Context, u User, code, botKey string, now int64) error {
	if !u.Active || u.TelegramID <= 0 || len(code) != 20 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM twilight_mini_sessions WHERE uid=$1 AND bot_hash=$2`, u.UID, identityDigest("shop-code|"+botKey))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO twilight_mini_sessions(token_hash,uid,proof_hash,nonce_hash,bot_hash,expires_at,created_at,telegram_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, identityDigest(code), u.UID, identityDigest("shop-code|"+code), identityDigest(code), identityDigest("shop-code|"+botKey), now+300, now, u.TelegramID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ConsumeShopLoginCode(ctx context.Context, code, botKey string, now int64) (User, error) {
	var uid, tgid int64
	err := s.db.QueryRowContext(ctx, `DELETE FROM twilight_mini_sessions WHERE token_hash=$1 AND bot_hash=$2 AND expires_at>$3 RETURNING uid,telegram_id`, identityDigest(strings.ToUpper(strings.TrimSpace(code))), identityDigest("shop-code|"+botKey), now).Scan(&uid, &tgid)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u, ok := s.User(uid)
	if !ok || !u.Active || tgid <= 0 || tgid != u.TelegramID {
		return User{}, ErrNotFound
	}
	return u, nil
}
