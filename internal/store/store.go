package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tradebot/internal/domain"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, symbol TEXT, side TEXT, qty REAL, price REAL,
  stop_price REAL, tp_price REAL, reason TEXT, binding TEXT, eff_risk_pct REAL
);
CREATE TABLE IF NOT EXISTS fills (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, symbol TEXT, side TEXT, qty REAL, price REAL, fee REAL
);
`

func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) RecordOrder(o domain.Order) error {
	_, err := s.db.Exec(
		`INSERT INTO orders(ts,symbol,side,qty,price,stop_price,tp_price,reason,binding,eff_risk_pct)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		o.Time, o.Symbol, o.Side.String(), o.Qty, o.Price, o.StopPrice, o.TPPrice, o.Reason, o.BindingConstraint, o.EffectiveRiskPct)
	return err
}

func (s *Store) RecordFill(f domain.Fill) error {
	_, err := s.db.Exec(
		`INSERT INTO fills(ts,symbol,side,qty,price,fee) VALUES(?,?,?,?,?,?)`,
		f.Time, f.Symbol, f.Side.String(), f.Qty, f.Price, f.Fee)
	return err
}

func (s *Store) CountFills() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM fills`).Scan(&n)
	return n, err
}
