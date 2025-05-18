CREATE TABLE
    IF NOT EXISTS users (
        id serial PRIMARY KEY,
        first_name character varying NOT NULL,
        last_name character varying NOT NULL,
        email character varying NOT NULL,
        password character varying NOT NULL,
        created_at timestamp NOT NULL DEFAULT now ()
    );