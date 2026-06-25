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
CREATE TABLE IF NOT EXISTS signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER, symbol TEXT, strategy TEXT, action TEXT, reason TEXT
);
CREATE TABLE IF NOT EXISTS pnl_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER, equity REAL, realized REAL
);
CREATE TABLE IF NOT EXISTS kill_state (
  id INTEGER PRIMARY KEY CHECK (id=1), killed INTEGER, reason TEXT, ts INTEGER
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

func (s *Store) RecordSignal(sym, strat, action, reason string, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO signals(ts,symbol,strategy,action,reason) VALUES(?,?,?,?,?)`,
		ts, sym, strat, action, reason)
	return err
}

func (s *Store) RecordPnLSnapshot(ts int64, equity, realized float64) error {
	_, err := s.db.Exec(`INSERT INTO pnl_snapshots(ts,equity,realized) VALUES(?,?,?)`, ts, equity, realized)
	return err
}

func (s *Store) SaveKillState(killed bool, reason string, ts int64) error {
	k := 0
	if killed {
		k = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO kill_state(id,killed,reason,ts) VALUES(1,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET killed=excluded.killed, reason=excluded.reason, ts=excluded.ts`,
		k, reason, ts)
	return err
}

func (s *Store) LoadKillState() (bool, error) {
	var k int
	err := s.db.QueryRow(`SELECT killed FROM kill_state WHERE id=1`).Scan(&k)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return k == 1, nil
}
