CREATE TABLE
    IF NOT EXISTS order_items (
        id serial PRIMARY KEY,
        order_id integer NOT NULL,
        product_id integer NOT NULL,
        quantity integer NOT NULL,
        price decimal(10, 2) NOT NULL
    );

ALTER TABLE order_items ADD CONSTRAINT order_order_item_fk FOREIGN KEY ("order_id") REFERENCES orders ("id") ON DELETE CASCADE ON UPDATE NO ACTION;

ALTER TABLE order_items ADD CONSTRAINT product_order_item_fk FOREIGN KEY ("product_id") REFERENCES products ("id") ON DELETE CASCADE ON UPDATE NO ACTION;