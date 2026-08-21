package sqlite

import "testing"

// A write that asks for the row it made.
//
// `INSERT ... RETURNING *` is how a flow answers with what it wrote, which a
// GraphQL mutation declaring a return type has to do. The statement ran and its
// rows were fetched and thrown away, because the question "does this return
// rows" looked only at the first word — while the branch it guards says, in its
// own comment, that it is there for RETURNING clauses. Postgres asked properly;
// the same statement behaved differently on the two drivers.

func TestAStatementThatReturnsRowsIsRecognised(t *testing.T) {
	returnsRows := []string{
		"SELECT * FROM users",
		"  select 1",
		"WITH recent AS (SELECT 1) SELECT * FROM recent",
		"INSERT INTO products (sku) VALUES (:sku) RETURNING *",
		"UPDATE products SET price = :price WHERE sku = :sku RETURNING sku, price",
		"DELETE FROM products WHERE sku = :sku returning sku",
	}
	for _, statement := range returnsRows {
		if !isSelectQuery(statement) {
			t.Errorf("returns rows and was not recognised: %s", statement)
		}
	}

	returnsNone := []string{
		"INSERT INTO products (sku) VALUES (:sku)",
		"UPDATE products SET price = :price WHERE sku = :sku",
		"DELETE FROM products WHERE sku = :sku",
		"CREATE TABLE products (sku TEXT)",
	}
	for _, statement := range returnsNone {
		if isSelectQuery(statement) {
			t.Errorf("returns no rows and was treated as if it did: %s", statement)
		}
	}
}
