-- ============================================================
-- Schema for SRE microservices project
-- Loaded once on first startup via Postgres' /docker-entrypoint-initdb.d
-- ============================================================

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    price       NUMERIC(12,2) NOT NULL CHECK (price >= 0),
    stock       INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    total      NUMERIC(12,2) NOT NULL,
    status     TEXT NOT NULL DEFAULT 'created',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);

CREATE TABLE IF NOT EXISTS order_items (
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id BIGINT NOT NULL REFERENCES products(id),
    quantity   INTEGER NOT NULL CHECK (quantity > 0),
    unit_price NUMERIC(12,2) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_id);

CREATE TABLE IF NOT EXISTS chat_messages (
    id          BIGSERIAL PRIMARY KEY,
    sender_id   BIGINT NOT NULL REFERENCES users(id),
    receiver_id BIGINT NOT NULL REFERENCES users(id),
    content     TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_pair ON chat_messages(sender_id, receiver_id);

-- Seed products so the storefront is not empty.
INSERT INTO products (name, description, price, stock) VALUES
    ('Mechanical Keyboard', '75% layout, hot-swappable switches', 129.99, 50),
    ('Wireless Mouse',      'Ergonomic, 2.4GHz + Bluetooth',     49.50,  120),
    ('27" 4K Monitor',      'IPS panel, 60Hz, USB-C hub',        399.00, 25),
    ('USB-C Hub',           '7-in-1 with HDMI and SD reader',     39.90, 200),
    ('Noise-Cancelling Headphones', 'Bluetooth, 30h battery',     199.00, 40)
ON CONFLICT DO NOTHING;
