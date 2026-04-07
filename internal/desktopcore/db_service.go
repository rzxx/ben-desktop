package desktopcore

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

const txCommitHooksKey = "desktopcore:tx-commit-hooks"

type DBService struct {
	db *gorm.DB
}

type txStateKey struct{}

type txState struct {
	commitHooks   *txCommitHookState
	oplogMutation any
}

type txCommitHookState struct {
	mu    sync.Mutex
	hooks []func()
}

func OpenDBService(path string) (*DBService, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := autoMigrate(db); err != nil {
		_ = closeSQL(db)
		return nil, err
	}
	return &DBService{db: db}, nil
}

func NewDBService(db *gorm.DB) *DBService {
	if db == nil {
		return nil
	}
	return &DBService{db: db}
}

func (s *DBService) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *DBService) WithContext(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx)
}

func (s *DBService) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if s == nil || s.db == nil {
		return nil
	}
	state := &txState{commitHooks: &txCommitHookState{}}
	ctx = context.WithValue(ctx, txStateKey{}, state)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
	if err != nil || state.commitHooks == nil {
		return err
	}
	state.commitHooks.run()
	return nil
}

func (s *DBService) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return closeSQL(s.db)
}

func registerTxCommitHook(tx *gorm.DB, hook func()) {
	if tx == nil || hook == nil {
		return
	}
	state := txCommitHooks(tx)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.hooks = append(state.hooks, hook)
}

func txCommitHooks(tx *gorm.DB) *txCommitHookState {
	if state := transactionState(tx); state != nil {
		if state.commitHooks == nil {
			state.commitHooks = &txCommitHookState{}
		}
		return state.commitHooks
	}
	if tx == nil {
		return &txCommitHookState{}
	}
	if existing, ok := tx.InstanceGet(txCommitHooksKey); ok {
		if state, stateOK := existing.(*txCommitHookState); stateOK && state != nil {
			return state
		}
	}
	state := &txCommitHookState{}
	tx.InstanceSet(txCommitHooksKey, state)
	return state
}

func transactionState(tx *gorm.DB) *txState {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil {
		return nil
	}
	state, _ := tx.Statement.Context.Value(txStateKey{}).(*txState)
	return state
}

func (s *txCommitHookState) run() {
	if s == nil {
		return
	}
	s.mu.Lock()
	hooks := append([]func(){}, s.hooks...)
	s.hooks = nil
	s.mu.Unlock()
	for _, hook := range hooks {
		if hook != nil {
			hook()
		}
	}
}
