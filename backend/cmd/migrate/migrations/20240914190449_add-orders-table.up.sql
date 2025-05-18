CREATE TYPE order_status_enum AS ENUM ('pending', 'completed', 'cancelled');

CREATE TABLE
    IF NOT EXISTS orders (
        id serial PRIMARY KEY,
        user_id integer NOT NULL,
        total decimal(10, 2) NOT NULL,
        status order_status_enum NOT NULL DEFAULT 'pending',
        address text NOT NULL,
        created_at timestamp NOT NULL DEFAULT now ()
    );

ALTER TABLE orders ADD CONSTRAINT user_order_fk FOREIGN KEY ("user_id") REFERENCES users ("id") ON DELETE CASCADE ON UPDATE NO ACTION;