package storage

import (
	"context"
	"errors"
	"time"
)

var ErrLessonChanged = errors.New("active lesson changed")

type Lesson struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) ActiveLesson(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='active_lesson'").Scan(&id)
	return id, err
}

func (s *Store) StartLesson(ctx context.Context, id, name string, nextPIN func() string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active, previousPIN string
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT value FROM settings WHERE key='active_lesson'),
 (SELECT value FROM settings WHERE key='last_lesson_pin')`).Scan(&active, &previousPIN); err != nil {
		return err
	}
	if active != "" {
		return ErrLessonChanged
	}
	pin := nextPIN()
	for pin == previousPIN {
		pin = nextPIN()
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO classrooms VALUES (?,?,?,'cloud-active')", id, name, time.Now().UnixMilli()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET value=CASE key
 WHEN 'active_lesson' THEN ? WHEN 'pin' THEN ? WHEN 'last_lesson_pin' THEN ?
 WHEN 'sending' THEN '1' ELSE CAST(value AS INTEGER)+1 END
 WHERE key IN ('active_lesson','pin','last_lesson_pin','sending','generation')`, id, pin, pin); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EndLesson(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := changed(tx.ExecContext(ctx, "UPDATE classrooms SET status='cloud-closed' WHERE id=? AND status='cloud-active' AND id=(SELECT value FROM settings WHERE key='active_lesson')", id))
	if err != nil {
		return err
	}
	if !updated {
		return ErrLessonChanged
	}
	if _, err := tx.ExecContext(ctx, "UPDATE students SET token_hash=NULL WHERE classroom_id=?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET value=CASE key
 WHEN 'active_lesson' THEN '' WHEN 'last_lesson_pin' THEN (SELECT value FROM settings WHERE key='pin') ELSE CAST(value AS INTEGER)+1 END
 WHERE key IN ('active_lesson','last_lesson_pin','generation')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClosedLesson(ctx context.Context, id string) (Lesson, error) {
	var lesson Lesson
	err := s.db.QueryRowContext(ctx, "SELECT id,name,created_at FROM classrooms WHERE id=? AND status='cloud-closed'", id).Scan(&lesson.ID, &lesson.Name, &lesson.CreatedAt)
	return lesson, err
}

func (s *Store) ClosedLessons(ctx context.Context, before string) ([]Lesson, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,created_at FROM classrooms WHERE status='cloud-closed' AND (?='' OR rowid<(SELECT rowid FROM classrooms WHERE id=? AND status='cloud-closed')) ORDER BY rowid DESC LIMIT 50", before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Lesson, 0)
	for rows.Next() {
		var lesson Lesson
		if err := rows.Scan(&lesson.ID, &lesson.Name, &lesson.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, lesson)
	}
	return result, rows.Err()
}

func (s *Store) Leave(ctx context.Context, id, tokenHash string) (bool, error) {
	return changed(s.db.ExecContext(ctx, `UPDATE students SET token_hash=NULL,
 approval_status=CASE approval_status WHEN 'pending' THEN 'denied' ELSE 'removed' END
 WHERE classroom_id=? AND id=? AND token_hash=? AND approval_status IN ('pending','approved')`, s.id, id, tokenHash))
}
