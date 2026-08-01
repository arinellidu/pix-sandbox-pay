-- S1 widens the charge projection to the fields the API Pix `cob` resource
-- carries. Added as columns rather than folded into 0001 so a database created
-- by S0 keeps working.
ALTER TABLE charges ADD COLUMN solicitacao_pagador TEXT NOT NULL DEFAULT '';
ALTER TABLE charges ADD COLUMN devedor_nome TEXT NOT NULL DEFAULT '';
ALTER TABLE charges ADD COLUMN devedor_cpf TEXT NOT NULL DEFAULT '';
ALTER TABLE charges ADD COLUMN devedor_cnpj TEXT NOT NULL DEFAULT '';
ALTER TABLE charges ADD COLUMN expiracao INTEGER NOT NULL DEFAULT 86400;
ALTER TABLE charges ADD COLUMN loc_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE charges ADD COLUMN revisao INTEGER NOT NULL DEFAULT 0;
