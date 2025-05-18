ALTER TABLE order_items
DROP CONSTRAINT order_order_item_fk;

ALTER TABLE order_items
DROP CONSTRAINT product_order_item_fk;

DROP TABLE IF EXISTS order_items;