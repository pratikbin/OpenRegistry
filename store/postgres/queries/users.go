// nolint
package queries

var (
	AddUser = `insert into users (id, is_active, username, name, email, password, hireable, html_url, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);`
	GetUser                 = `select id, is_active, username, email, created_at, updated_at from users where email=$1 or username=$1;`
	GetUserWithPassword     = `select id, is_active, username, email, password, created_at, updated_at from users where email=$1 or username=$1;`
	GetUserById             = `select id, is_active, username, email, created_at, updated_at from users where id=$1;`
	GetUserByIdWithPassword = `select id, is_active, username, email, password, created_at, updated_at from users where id=$1;`
	GetUserWithSession      = `select id, is_active, name, username, email, hireable, html_url, created_at, updated_at from users where id=(select owner from session where id=$1);`
	UpdateUser              = `update users set is_active = $1, updated_at = $2 where id = $3;`
	SetUserActive           = `update users set is_active=true where id=$1`
	DeleteUser              = `delete from users where username = $1;`
	UpdateUserPwd           = `update users set password=$1 where id=$2;`
	GetAllEmails            = `select email from users;`
	AddOAuthUser            = `insert into users (id, username, email, html_url, created_at, updated_at,
bio, type, gravatar_id, login, name, node_id, avatar_url, oauth_id, is_active, hireable)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) on conflict (email) do update set username=$2, email=$3`
	UpdateUserInstallationID = `update users set github_app_installation_id=$1 where username=$2;`
	GetUserInstallationID    = `select github_app_installation_id from users where username=$1;`
)

var (
	AddSession        = `insert into session (id,refresh_token,owner) values($1, $2, (select id from users where username=$3));`
	GetSession        = `select id,refresh_token,owner from session where id=$1;`
	DeleteSession     = `delete from session where id=$1 and owner=$2;`
	DeleteAllSessions = `delete from session where owner=$1;`
)
