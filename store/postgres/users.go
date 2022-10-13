package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/containerish/OpenRegistry/store/postgres/queries"
	"github.com/containerish/OpenRegistry/types"
	"github.com/google/uuid"
)

func (p *pg) AddUser(ctx context.Context, u *types.User) error {
	if err := u.Validate(); err != nil {
		return err
	}

	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	t := time.Now()
	if u.Id == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return fmt.Errorf("error creating id for add user: %w", err)
		}
		u.Id = id.String()
	}
	_, err := p.conn.Exec(
		childCtx,
		queries.AddUser,
		u.Id,
		u.IsActive,
		u.Username,
		u.Name,
		u.Email,
		u.Password,
		u.Hireable,
		u.HTMLURL,
		t,
		t,
	)
	if err != nil {
		return fmt.Errorf("error adding user to database: %w", err)
	}

	return nil
}

func (p *pg) AddOAuthUser(ctx context.Context, u *types.User) error {
	if err := u.Validate(); err != nil {
		return err
	}

	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	t := time.Now()
	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("error creating id for oauth user")
	}

	_, err = p.conn.Exec(
		childCtx,
		queries.AddOAuthUser,
		id.String(),
		u.Username,
		u.Email,
		u.HTMLURL,
		t,
		t,
		u.Bio,
		u.Type,
		u.GravatarID,
		u.Login,
		u.Name,
		u.NodeID,
		u.AvatarURL,
		u.OAuthID,
		u.IsActive,
		u.Hireable,
	)
	if err != nil {
		return fmt.Errorf("error adding user to database: %w", err)
	}

	return nil
}

func (p *pg) GetUser(ctx context.Context, identifier string, withPassword bool) (*types.User, error) {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	var user types.User
	if withPassword {
		row := p.conn.QueryRow(childCtx, queries.GetUserWithPassword, identifier)

		err := row.Scan(
			&user.Id,
			&user.IsActive,
			&user.Username,
			&user.Email,
			&user.Password,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("ERR_GET_USER_WITH_PASSWORD_FROM_DB: %w", err)
		}

		return &user, nil
	}

	row := p.conn.QueryRow(childCtx, queries.GetUser, identifier)
	err := row.Scan(
		&user.Id,
		&user.IsActive,
		&user.Username,
		&user.Email,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("ERR_GET_USER_FROM_DB: %w", err)
	}

	return &user, nil
}

func (p *pg) GetUserById(ctx context.Context, userId string, withPassword bool) (*types.User, error) {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	if withPassword {
		row := p.conn.QueryRow(childCtx, queries.GetUserByIdWithPassword, userId)

		var user types.User
		if err := row.Scan(
			&user.Id,
			&user.IsActive,
			&user.Username,
			&user.Email,
			&user.Password,
			&user.CreatedAt,
			&user.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ERR_GET_USER_BY_ID_PWD_HASH: %w", err)
		}

		return &user, nil
	}

	row := p.conn.QueryRow(childCtx, queries.GetUserById, userId)
	var user types.User
	err := row.Scan(
		&user.Id,
		&user.IsActive,
		&user.Username,
		&user.Email,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("ERR_GET_USER_BY_ID: %w", err)
	}

	return &user, nil
}

func (p *pg) GetUserWithSession(ctx context.Context, sessionId string) (*types.User, error) {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	row := p.conn.QueryRow(childCtx, queries.GetUserWithSession, sessionId)

	var user types.User
	if err := row.Scan(
		&user.Id,
		&user.IsActive,
		&user.Name,
		&user.Username,
		&user.Email,
		&user.Hireable,
		&user.HTMLURL,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("ERR_SESSION_NOT_FOUND: %w", err)
	}

	return &user, nil
}

// UpdateUser
// update users set username = $1, email = $2, updated_at = $3 where username = $4
func (p *pg) UpdateUser(ctx context.Context, userId string, u *types.User) error {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	t := time.Now()
	_, err := p.conn.Exec(childCtx, queries.UpdateUser, u.IsActive, t, userId)
	if err != nil {
		return fmt.Errorf("error updating user: %s", err)
	}
	return nil
}

func (p *pg) UpdateUserPWD(ctx context.Context, identifier string, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("insufficient feilds for updating user")
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	_, err := p.conn.Exec(childCtx, queries.UpdateUserPwd, newPassword, identifier)
	if err != nil {
		return fmt.Errorf("error updating user: %s", err)
	}
	return nil
}

func (p *pg) UpdateInstallationID(ctx context.Context, id, githubUsername string) error {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	_, err := p.conn.Exec(childCtx, queries.UpdateUserInstallationID, id, githubUsername)
	if err != nil {
		return fmt.Errorf("error updating github app installation id: %w", err)
	}

	return nil
}

func (p *pg) GetInstallationID(ctx context.Context, githubUsername string) (string, error) {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	row := p.conn.QueryRow(childCtx, queries.GetUserInstallationID, githubUsername)

	var installationID string
	if err := row.Scan(&installationID); err != nil {
		return "", fmt.Errorf("error getting github app installation id: %w", err)
	}

	return installationID, nil

}

func (p *pg) AddVerifyEmail(ctx context.Context, token, userId string) error {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()
	_, err := p.conn.Exec(childCtx, queries.AddVerifyUser, token, userId)
	if err != nil {
		return fmt.Errorf("error adding verify link: %w", err)
	}
	return nil
}

func (p *pg) GetVerifyEmail(ctx context.Context, token string) (string, error) {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	row := p.conn.QueryRow(childCtx, queries.GetVerifyUser, token)
	if row == nil {
		return "", fmt.Errorf("could not find verify token for userId")
	}

	var userId string
	err := row.Scan(&userId)
	if err != nil {
		return "", fmt.Errorf("error scanning verify token: %w", err)
	}

	return userId, nil
}

func (p *pg) DeleteVerifyEmail(ctx context.Context, token string) error {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	_, err := p.conn.Exec(childCtx, queries.DeleteVerifyUser, token)
	if err != nil {
		return fmt.Errorf("error deleting verify token: %w", err)
	}

	return nil
}

// DeleteUser - delete from user where username = $1;
func (p *pg) DeleteUser(ctx context.Context, identifier string) error {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	_, err := p.conn.Exec(childCtx, queries.DeleteUser, identifier)
	if err != nil {
		return fmt.Errorf("error deleting user: %s", identifier)
	}
	return nil
}

// IsActive - if the user has logged in, isActive returns true
// this method is also useful for limiting access of malicious actors
func (p *pg) IsActive(ctx context.Context, identifier string) bool {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()
	row := p.conn.QueryRow(childCtx, queries.GetUser, identifier)
	return row != nil
}

func (p *pg) UserExists(ctx context.Context, id string) bool {
	childCtx, cancel := context.WithTimeout(ctx, time.Millisecond*100)
	defer cancel()

	row, err := p.GetUserById(childCtx, id, false)
	if err != nil || row == nil {
		return false
	}

	return true
}
