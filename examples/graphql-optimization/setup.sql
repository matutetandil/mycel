-- Database setup for GraphQL Optimization Demo
-- Run: sqlite3 demo.db < setup.sql

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    avatar TEXT,
    bio TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

-- Products table
CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY,
    sku TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT
);

-- Orders table
CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    total REAL NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- Prices table (simulates external pricing service)
CREATE TABLE IF NOT EXISTS prices (
    product_id TEXT PRIMARY KEY,
    price REAL NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    discount REAL DEFAULT 0,
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- Inventory table (simulates external inventory service)
CREATE TABLE IF NOT EXISTS inventory (
    product_id TEXT PRIMARY KEY,
    stock INTEGER NOT NULL DEFAULT 0,
    warehouse TEXT,
    reserved INTEGER DEFAULT 0,
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- Reviews table (simulates external reviews service)
CREATE TABLE IF NOT EXISTS reviews (
    id TEXT PRIMARY KEY,
    product_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    rating INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    comment TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    FOREIGN KEY (product_id) REFERENCES products(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Sample data: Users
INSERT OR IGNORE INTO users (id, email, name, avatar, bio) VALUES
    ('user-1', 'alice@example.com', 'Alice Johnson', 'https://i.pravatar.cc/150?u=alice', 'Software engineer and coffee enthusiast'),
    ('user-2', 'bob@example.com', 'Bob Smith', 'https://i.pravatar.cc/150?u=bob', 'Product designer'),
    ('user-3', 'carol@example.com', 'Carol Williams', 'https://i.pravatar.cc/150?u=carol', 'Data scientist');

-- Sample data: Products
INSERT OR IGNORE INTO products (id, sku, name, description, category) VALUES
    ('prod-1', 'LAPTOP-PRO-15', 'Pro Laptop 15"', 'High-performance laptop with M2 chip', 'Electronics'),
    ('prod-2', 'HEADPHONES-NC', 'Noise Cancelling Headphones', 'Premium wireless headphones', 'Electronics'),
    ('prod-3', 'KEYBOARD-MECH', 'Mechanical Keyboard', 'RGB mechanical keyboard with Cherry MX switches', 'Accessories'),
    ('prod-4', 'MOUSE-ERGO', 'Ergonomic Mouse', 'Wireless ergonomic mouse', 'Accessories'),
    ('prod-5', 'MONITOR-4K', '4K Monitor 27"', 'Ultra HD monitor with HDR support', 'Electronics');

-- Sample data: Prices (external pricing service)
INSERT OR IGNORE INTO prices (product_id, price, currency, discount) VALUES
    ('prod-1', 1999.99, 'USD', 0.10),
    ('prod-2', 349.99, 'USD', 0),
    ('prod-3', 149.99, 'USD', 0.15),
    ('prod-4', 79.99, 'USD', 0),
    ('prod-5', 599.99, 'USD', 0.05);

-- Sample data: Inventory (external inventory service)
INSERT OR IGNORE INTO inventory (product_id, stock, warehouse, reserved) VALUES
    ('prod-1', 50, 'Warehouse A', 5),
    ('prod-2', 200, 'Warehouse B', 20),
    ('prod-3', 150, 'Warehouse A', 10),
    ('prod-4', 300, 'Warehouse C', 0),
    ('prod-5', 75, 'Warehouse B', 8);

-- Sample data: Reviews (external reviews service)
INSERT OR IGNORE INTO reviews (id, product_id, user_id, rating, comment) VALUES
    ('rev-1', 'prod-1', 'user-1', 5, 'Amazing laptop, very fast!'),
    ('rev-2', 'prod-1', 'user-2', 4, 'Great but expensive'),
    ('rev-3', 'prod-2', 'user-1', 5, 'Best headphones ever'),
    ('rev-4', 'prod-2', 'user-3', 4, 'Good noise cancellation'),
    ('rev-5', 'prod-3', 'user-2', 5, 'Perfect for coding'),
    ('rev-6', 'prod-3', 'user-3', 5, 'Love the RGB!'),
    ('rev-7', 'prod-4', 'user-1', 3, 'Decent but not great'),
    ('rev-8', 'prod-5', 'user-2', 5, 'Crystal clear display');

-- Sample data: Orders
INSERT OR IGNORE INTO orders (id, user_id, product_id, quantity, total, status) VALUES
    ('order-1', 'user-1', 'prod-1', 1, 1799.99, 'completed'),
    ('order-2', 'user-1', 'prod-3', 2, 254.98, 'completed'),
    ('order-3', 'user-2', 'prod-2', 1, 349.99, 'shipped'),
    ('order-4', 'user-2', 'prod-4', 1, 79.99, 'pending'),
    ('order-5', 'user-3', 'prod-5', 1, 569.99, 'processing'),
    ('order-6', 'user-3', 'prod-1', 1, 1799.99, 'completed');

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_product_id ON orders(product_id);
CREATE INDEX IF NOT EXISTS idx_reviews_product_id ON reviews(product_id);
