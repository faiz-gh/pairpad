CREATE TABLE
    IF NOT EXISTS products (
        id serial PRIMARY KEY,
        name character varying NOT NULL,
        description text NOT NULL,
        image character varying NOT NULL,
        price decimal(10, 2) NOT NULL,
        quantity integer NOT NULL,
        created_at timestamp NOT NULL DEFAULT now ()
    );