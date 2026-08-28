-- Copyright 2026 Muhammad Salah
-- SPDX-License-Identifier: Apache-2.0
--
-- One database per Keycloak. They share a server because three servers would
-- cost three times the memory for a difference PortCloak cannot observe, and
-- they do not share a database because a restore into kc-merged must not be
-- able to reach kc-a's rows by accident.
CREATE DATABASE kc_a OWNER keycloak;
CREATE DATABASE kc_b OWNER keycloak;
CREATE DATABASE kc_merged OWNER keycloak;
