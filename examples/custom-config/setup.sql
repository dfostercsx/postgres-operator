--- System Setup
SET application_name="container_setup";

CREATE EXTENSION IF NOT EXISTS pgaudit;

CREATE USER PGHA_USER LOGIN;
ALTER USER PGHA_USER PASSWORD 'PGHA_USER_PASSWORD';

CREATE DATABASE PGHA_DATABASE;
GRANT ALL PRIVILEGES ON DATABASE PGHA_DATABASE TO PGHA_USER;

CREATE USER testuser2 LOGIN;
ALTER USER testuser2 PASSWORD 'customconfpass';

CREATE DATABASE PGHA_DATABASE;
GRANT ALL PRIVILEGES ON DATABASE PGHA_DATABASE TO testuser2;

--- PGHA_DATABASE Setup

\c PGHA_DATABASE

CREATE EXTENSION IF NOT EXISTS pgaudit;

CREATE SCHEMA IF NOT EXISTS PGHA_USER;

/* The following has been customized for the custom-config example */

SET SESSION AUTHORIZATION PGHA_USER;

CREATE TABLE custom_config_table (
	KEY VARCHAR(30) PRIMARY KEY,
	VALUE VARCHAR(50) NOT NULL,
	UPDATEDT TIMESTAMP NOT NULL
);

INSERT INTO custom_config_table (KEY, VALUE, UPDATEDT) VALUES ('CPU', '256', now());

GRANT ALL ON custom_config_table TO testuser2;
