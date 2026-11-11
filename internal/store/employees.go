// Package store holds pgx repositories of the app schema.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
)

// Employees is EmployeeProvider over app.employees (start dates included).
type Employees struct{ Pool *pgxpool.Pool }

const employeeCols = `user_id, display_name, role, COALESCE(track_id, ''), start_date, COALESCE(mentor_id, '')`

func scanEmployee(row pgx.Row) (employees.Employee, error) {
	var e employees.Employee
	var start *time.Time
	if err := row.Scan(&e.UserID, &e.DisplayName, &e.Role, &e.Track, &start, &e.MentorID); err != nil {
		return e, err
	}
	if start != nil {
		d := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		e.StartDate = &d
	}
	return e, nil
}

// Get returns an employee.
func (s *Employees) Get(ctx context.Context, id string) (employees.Employee, error) {
	e, err := scanEmployee(s.Pool.QueryRow(ctx, `SELECT `+employeeCols+` FROM app.employees WHERE user_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, employees.ErrNotFound
	}
	return e, err
}

// InternsOf returns the interns of a mentor.
func (s *Employees) InternsOf(ctx context.Context, mentorID string) ([]employees.Employee, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+employeeCols+` FROM app.employees
		WHERE mentor_id = $1 AND role = 'intern' ORDER BY user_id`, mentorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []employees.Employee
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// All returns every employee ordered by role and id (login screen).
func (s *Employees) All(ctx context.Context) ([]employees.Employee, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+employeeCols+` FROM app.employees ORDER BY role DESC, user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []employees.Employee
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
