package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const ClassroomID = "classroom"
const TeacherID = "teacher"

type Store struct {
	db *sql.DB
	id string
}

func (s *Store) ForClass(id string) *Store { return &Store{db: s.db, id: id} }
func (s *Store) ClassID() string           { return s.id }

const schema = `
CREATE TABLE IF NOT EXISTS classrooms (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 status TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS classrooms_status ON classrooms(status, created_at DESC);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS students (
 id TEXT PRIMARY KEY,
 classroom_id TEXT NOT NULL REFERENCES classrooms(id),
 display_name TEXT NOT NULL,
 approval_status TEXT NOT NULL CHECK (approval_status IN ('pending','approved','denied','removed')),
 token_hash TEXT UNIQUE,
 created_at INTEGER NOT NULL,
 last_seen_at INTEGER NOT NULL DEFAULT 0,
 teacher_seen_seq INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS students_classroom_status ON students(classroom_id, approval_status);
CREATE TABLE IF NOT EXISTS messages (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT UNIQUE NOT NULL,
 classroom_id TEXT NOT NULL REFERENCES classrooms(id),
 sender_id TEXT NOT NULL,
 receiver_id TEXT NOT NULL,
 type TEXT NOT NULL CHECK (type IN ('text','code')),
 content TEXT NOT NULL,
 language TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 request_id TEXT NOT NULL,
 UNIQUE (classroom_id, sender_id, request_id)
);
CREATE INDEX IF NOT EXISTS messages_sender ON messages(classroom_id, sender_id, seq);
CREATE INDEX IF NOT EXISTS messages_receiver ON messages(classroom_id, receiver_id, seq);
CREATE TABLE IF NOT EXISTS message_deliveries (
 message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 recipient_id TEXT NOT NULL,
 delivered_at INTEGER,
 read_at INTEGER,
 PRIMARY KEY (message_id, recipient_id)
);
CREATE INDEX IF NOT EXISTS deliveries_recipient ON message_deliveries(recipient_id, message_id);
INSERT OR IGNORE INTO classrooms VALUES ('classroom','我的课堂',CAST(strftime('%s','now') AS INTEGER) * 1000,'active');
INSERT OR IGNORE INTO settings VALUES ('sending','1'), ('pin',''), ('generation','1'), ('ip',''), ('stopped','0'), ('active_lesson',''), ('last_lesson_pin','');
`

func Open(dir string) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "classrelay.sqlite")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteURI(filepath.ToSlash(path)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化 SQLite：%w", err)
	}
	return &Store{db: db, id: ClassroomID}, nil
}

func sqliteURI(path string) string {
	// An absolute Windows drive path must follow an empty URI authority.
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := url.URL{
		Scheme: "file", Path: path,
		RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
	}
	return dsn.String()
}

func (s *Store) Close() error { return s.db.Close() }

type Settings struct {
	Name       string `json:"name"`
	PIN        string `json:"pin,omitempty"`
	Sending    bool   `json:"sending"`
	Generation int64  `json:"generation"`
	IP         string `json:"-"`
	Stopped    bool   `json:"-"`
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var v Settings
	err := s.db.QueryRowContext(ctx, `
SELECT name,(SELECT value FROM settings WHERE key='pin'),
 (SELECT value='1' FROM settings WHERE key='sending'),
 (SELECT CAST(value AS INTEGER) FROM settings WHERE key='generation'),
 (SELECT value FROM settings WHERE key='ip'),
 (SELECT value='1' FROM settings WHERE key='stopped')
FROM classrooms WHERE id=?`, s.id).Scan(&v.Name, &v.PIN, &v.Sending, &v.Generation, &v.IP, &v.Stopped)
	return v, err
}

func (s *Store) SetNetwork(ctx context.Context, ip string, stopped bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE settings SET value=CASE key WHEN 'ip' THEN ? ELSE CAST(? AS TEXT) END WHERE key IN ('ip','stopped')", ip, stopped)
	return err
}

func (s *Store) SetSettings(ctx context.Context, name, pin string, sending bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE classrooms SET name=? WHERE id=?", name, s.id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE settings SET value=CASE key WHEN 'pin' THEN ? ELSE CAST(? AS TEXT) END WHERE key IN ('pin','sending')", pin, sending); err != nil {
		return err
	}
	return tx.Commit()
}

type Student struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	LastSeen int64  `json:"lastSeen"`
	Unread   int    `json:"unread"`
	Online   bool   `json:"online"`
}

func (s *Store) Join(ctx context.Context, id, name, digest string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
INSERT INTO students (id,classroom_id,display_name,approval_status,token_hash,created_at)
SELECT ?,?,?,'pending',?,?
WHERE (SELECT COUNT(*) FROM students WHERE classroom_id=? AND approval_status IN ('pending','approved')) < 500`,
		id, s.id, name, digest, time.Now().UnixMilli(), s.id)
	return changed(result, err)
}

func (s *Store) StudentByToken(ctx context.Context, digest string) (Student, error) {
	var v Student
	err := s.db.QueryRowContext(ctx, `SELECT id,display_name,approval_status,last_seen_at FROM students WHERE classroom_id=? AND token_hash=?`,
		s.id, digest).Scan(&v.ID, &v.Name, &v.Status, &v.LastSeen)
	return v, err
}

func (s *Store) StudentStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, "SELECT approval_status FROM students WHERE classroom_id=? AND id=?", s.id, id).Scan(&status)
	return status, err
}

func (s *Store) Students(ctx context.Context) ([]Student, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, display_name, approval_status,last_seen_at,
 (SELECT COUNT(*) FROM messages WHERE classroom_id=students.classroom_id AND sender_id=students.id AND seq>teacher_seen_seq)
 FROM students WHERE classroom_id=? AND approval_status!='denied'
 ORDER BY created_at,id`, s.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Student, 0)
	for rows.Next() {
		var v Student
		if err := rows.Scan(&v.ID, &v.Name, &v.Status, &v.LastSeen, &v.Unread); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func changed(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (s *Store) Decide(ctx context.Context, id, status, current string) (bool, error) {
	return changed(s.db.ExecContext(ctx, "UPDATE students SET approval_status=? WHERE classroom_id=? AND id=? AND approval_status=?", status, s.id, id, current))
}

func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	return changed(s.db.ExecContext(ctx, "UPDATE students SET display_name=? WHERE classroom_id=? AND id=? AND approval_status='approved'", name, s.id, id))
}

func (s *Store) Touch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE students SET last_seen_at=? WHERE classroom_id=? AND id=? AND approval_status='approved'", time.Now().UnixMilli(), s.id, id)
	return err
}

func (s *Store) Seen(ctx context.Context, id string, seq int64) (bool, error) {
	return changed(s.db.ExecContext(ctx, `
UPDATE students SET teacher_seen_seq=?
WHERE classroom_id=? AND id=? AND teacher_seen_seq<?
 AND EXISTS (SELECT 1 FROM messages WHERE classroom_id=? AND seq=? AND sender_id=? AND receiver_id='teacher')
`, seq, s.id, id, seq, s.id, seq, id))
}

type Message struct {
	Seq         int64  `json:"seq"`
	ID          string `json:"id"`
	ClassroomID string `json:"classroomId"`
	SenderID    string `json:"senderId"`
	ReceiverID  string `json:"receiverId"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Language    string `json:"language,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	RequestID   string `json:"requestId,omitempty"`
	Delivered   int    `json:"delivered"`
	Recipients  int    `json:"recipients"`
}

type scanner interface{ Scan(...any) error }

const columns = `m.seq,m.id,m.classroom_id,m.sender_id,m.receiver_id,m.type,m.content,m.language,m.created_at,m.request_id,
 (SELECT COUNT(*) FROM message_deliveries d WHERE d.message_id=m.id AND d.delivered_at IS NOT NULL),
 (SELECT COUNT(*) FROM message_deliveries d WHERE d.message_id=m.id)`

func scanMessage(row scanner) (Message, error) {
	var v Message
	err := row.Scan(&v.Seq, &v.ID, &v.ClassroomID, &v.SenderID, &v.ReceiverID, &v.Type, &v.Content, &v.Language, &v.CreatedAt, &v.RequestID, &v.Delivered, &v.Recipients)
	return v, err
}

func (s *Store) Send(ctx context.Context, msg Message) (Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO messages(id,classroom_id,sender_id,receiver_id,type,content,language,created_at,request_id)
 VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(classroom_id,sender_id,request_id) DO NOTHING`,
		msg.ID, s.id, msg.SenderID, msg.ReceiverID, msg.Type, msg.Content, msg.Language, time.Now().UnixMilli(), msg.RequestID)
	inserted, err := changed(result, err)
	if err != nil {
		return Message{}, err
	}
	if inserted {
		if msg.ReceiverID == "all" {
			_, err = tx.ExecContext(ctx, `INSERT INTO message_deliveries(message_id,recipient_id)
 SELECT ?,id FROM students WHERE classroom_id=? AND approval_status='approved'`, msg.ID, s.id)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO message_deliveries(message_id,recipient_id) VALUES (?,?)", msg.ID, msg.ReceiverID)
		}
		if err != nil {
			return Message{}, err
		}
	}
	saved, err := scanMessage(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM messages m WHERE m.classroom_id=? AND m.sender_id=? AND m.request_id=?", s.id, msg.SenderID, msg.RequestID))
	if err != nil {
		return Message{}, err
	}
	return saved, tx.Commit()
}

var opaqueID = regexp.MustCompile("^[a-zA-Z0-9_-]{8,100}$")

func ValidID(value string) bool { return opaqueID.MatchString(value) }

type Page struct {
	Messages   []Message `json:"messages"`
	More       bool      `json:"more"`
	Generation int64     `json:"generation"`
}

func (s *Store) History(ctx context.Context, actor, conversation string, cursor int64, forward bool) (Page, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	page := Page{Messages: make([]Message, 0)}
	if err := tx.QueryRowContext(ctx, "SELECT CAST(value AS INTEGER) FROM settings WHERE key='generation'").Scan(&page.Generation); err != nil {
		return page, err
	}

	where := "m.classroom_id=?"
	args := []any{s.id}
	switch {
	case actor != TeacherID:
		where += " AND (m.sender_id=? OR EXISTS (SELECT 1 FROM message_deliveries d WHERE d.message_id=m.id AND d.recipient_id=?))"
		args = append(args, actor, actor)
	case conversation == "all":
		where += " AND m.sender_id='teacher' AND m.receiver_id='all'"
	default:
		where += " AND ((m.sender_id='teacher' AND m.receiver_id=?) OR (m.sender_id=? AND m.receiver_id='teacher'))"
		args = append(args, conversation, conversation)
	}
	order := "DESC"
	if forward {
		order = "ASC"
		where += " AND m.seq>?"
		args = append(args, cursor)
	} else if cursor > 0 {
		where += " AND m.seq<?"
		args = append(args, cursor)
	}
	const size = 40
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM messages m WHERE "+where+" ORDER BY m.seq "+order+" LIMIT 41", args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return page, err
		}
		if len(page.Messages) == size {
			page.More = true
			break
		}
		if m.SenderID != actor {
			m.RequestID = ""
			m.Delivered, m.Recipients = 0, 0
		}
		page.Messages = append(page.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	if !forward {
		for left, right := 0, len(page.Messages)-1; left < right; left, right = left+1, right-1 {
			page.Messages[left], page.Messages[right] = page.Messages[right], page.Messages[left]
		}
	}
	return page, tx.Commit()
}

func idArgs(first any, ids []string) ([]any, string) {
	args := make([]any, 1, len(ids)+1)
	args[0] = first
	for _, id := range ids {
		args = append(args, id)
	}
	return args, strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
}

func (s *Store) Ack(ctx context.Context, actor string, ids []string) (bool, error) {
	args, placeholders := idArgs(actor, ids)
	args = append([]any{time.Now().UnixMilli(), s.id}, args...)
	return changed(s.db.ExecContext(ctx, "UPDATE message_deliveries SET delivered_at=? WHERE message_id IN (SELECT id FROM messages WHERE classroom_id=?) AND recipient_id=? AND delivered_at IS NULL AND message_id IN ("+placeholders+")", args...))
}

type Receipt struct {
	ID         string `json:"id"`
	Delivered  int    `json:"delivered"`
	Recipients int    `json:"recipients"`
}

func (s *Store) Receipts(ctx context.Context, actor string, ids []string) ([]Receipt, error) {
	args, placeholders := idArgs(actor, ids)
	args = append([]any{s.id}, args...)
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,COUNT(d.delivered_at),COUNT(d.message_id)
 FROM messages m LEFT JOIN message_deliveries d ON d.message_id=m.id
 WHERE m.classroom_id=? AND m.sender_id=? AND m.id IN (`+placeholders+") GROUP BY m.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Receipt, 0)
	for rows.Next() {
		var item Receipt
		if err := rows.Scan(&item.ID, &item.Delivered, &item.Recipients); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Clear(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE classroom_id=?", s.id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE settings SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FindRequest(ctx context.Context, actor, requestID string) (Message, error) {
	return scanMessage(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM messages m WHERE m.classroom_id=? AND m.sender_id=? AND m.request_id=?", s.id, actor, requestID))
}
