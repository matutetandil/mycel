package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Roles are the part of a user that authorization is decided on: the middleware
// matches them against a path rule, the JWT carries them to every service
// downstream, and a flow reads them as auth.roles.
//
// Neither SQL user store wrote them or read them. A service holding its users
// in memory kept roles because the whole struct is kept; the same service
// pointed at a database got every user back with an empty list, so every role
// rule refused and every claim was absent — silently, since an empty list is
// not an error anywhere.
//
// The column is opt-in rather than assumed. A users table that already exists
// need not grow one, and asking for a column nobody created would turn a
// working service into one that cannot read its own users. Writing
// `fields { roles = "roles" }` is what turns it on.

// rolesColumn returns the column roles are stored in, or "" when the
// configuration names none.
func rolesColumn(fields *FieldsConfig) string {
	if fields == nil {
		return ""
	}
	return fields.Roles
}

// passwordChangedColumn returns the column recording when a password was last
// set, or "" when the configuration names none.
//
// Opt-in for the same reason as roles: a users table that already exists need
// not grow a column, and selecting one nobody created would turn a working
// service into one that cannot read its own users. Writing
// `fields { password_changed_at = "..." }` is what turns password expiry on
// for a SQL-backed store.
func passwordChangedColumn(fields *FieldsConfig) string {
	if fields == nil {
		return ""
	}
	return fields.PasswordChangedAt
}

// optionalColumns are the columns a users table may or may not have.
type optionalColumns struct {
	roles           string
	passwordChanged string
}

func optionalUserColumns(fields *FieldsConfig) optionalColumns {
	return optionalColumns{
		roles:           rolesColumn(fields),
		passwordChanged: passwordChangedColumn(fields),
	}
}

// storedOptionals holds what those columns read back as, each null until a
// column that exists says otherwise.
type storedOptionals struct {
	roles           sql.NullString
	passwordChanged sql.NullTime
}

// userColumns lists the columns a user is read from, and reports which of the
// optional ones are among them so the caller knows what to expect back.
func userColumns(fields *FieldsConfig) (columns string, optional optionalColumns) {
	columns = fmt.Sprintf("%s, %s, %s, %s, %s",
		fields.ID, fields.Email, fields.PasswordHash, fields.CreatedAt, fields.UpdatedAt)
	optional = optionalUserColumns(fields)
	if optional.roles != "" {
		columns += ", " + optional.roles
	}
	if optional.passwordChanged != "" {
		columns += ", " + optional.passwordChanged
	}
	return columns, optional
}

// userScanTargets pairs those columns with the fields they are read into.
func userScanTargets(user *User, optional optionalColumns) ([]interface{}, *storedOptionals) {
	targets := []interface{}{&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt}
	stored := &storedOptionals{}
	if optional.roles != "" {
		targets = append(targets, &stored.roles)
	}
	if optional.passwordChanged != "" {
		targets = append(targets, &stored.passwordChanged)
	}
	return targets, stored
}

// applyStoredOptionals puts those columns onto the user, if there were any. A
// null column is a user with no roles, or one whose password has no recorded
// age, rather than an error.
func applyStoredOptionals(user *User, optional optionalColumns, stored *storedOptionals) {
	if optional.roles != "" && stored.roles.Valid {
		user.Roles = decodeRoles(stored.roles.String)
	}
	if optional.passwordChanged != "" && stored.passwordChanged.Valid {
		changed := stored.passwordChanged.Time
		user.PasswordChangedAt = &changed
	}
}

// encodeRoles renders a role list for storage.
//
// JSON, because it survives a role containing a comma and is what a jsonb or
// json column expects. A text column holds it just as well.
func encodeRoles(roles []string) string {
	if len(roles) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(roles)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// decodeRoles reads a role list back.
//
// A column that already exists is as likely to hold a comma-separated list as
// JSON — it is somebody else's table — so both are read. Anything that is
// neither is one role.
func decodeRoles(stored string) []string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return nil
	}

	if strings.HasPrefix(stored, "[") {
		var roles []string
		if err := json.Unmarshal([]byte(stored), &roles); err == nil {
			return trimmedNonEmpty(roles)
		}
	}

	return trimmedNonEmpty(strings.Split(stored, ","))
}

func trimmedNonEmpty(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
