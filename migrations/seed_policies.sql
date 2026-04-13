INSERT INTO policies (policy_id, type, priority, selectors, steps, condition) VALUES

-- BASELINE POLICIES (основа маршрутов)

-- Любое приложение в PROD → согласование менеджера
('base-app-prod', 'baseline', 100,
 '{"resource_type":"app","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1}]',
 NULL),

-- Любая база данных в PROD → менеджер + DBA
('base-db-prod', 'baseline', 100,
 '{"resource_type":"database","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"DBA Review","approvers":{"static":["dba-team"],"dynamic":[]},"mode":"ANY","order":2}]',
 NULL),

-- Серверы в PROD → менеджер + инфра-тим
('base-server-prod', 'baseline', 100,
 '{"resource_type":"server","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"Infra Review","approvers":{"static":["infra-lead"],"dynamic":[]},"mode":"ANY","order":2}]',
 NULL),

-- Любой ресурс в DEV → авто-согласование (менеджер)
('base-any-dev', 'baseline', 50,
 '{"resource_type":"*","environment":"DEV"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1}]',
 NULL),

-- STAGE → менеджер
('base-any-stage', 'baseline', 60,
 '{"resource_type":"*","environment":"STAGE"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1}]',
 NULL),

-- AUGMENT POLICIES (добавляют шаги)

-- Безопасность для app в PROD
('augment-security-app', 'augment', 90,
 '{"resource_type":"app","environment":"PROD"}',
 '[{"name":"Security Review","approvers":{"static":["security-team"],"dynamic":[]},"mode":"ALL","order":2}]',
 NULL),

-- Роль admin → дополнительное согласование
('augment-admin-role', 'augment', 120,
 '{"resource_type":"*","roles":["admin"]}',
 '[{"name":"Admin Approval","approvers":{"static":["admin-approver"],"dynamic":[]},"mode":"ALL","order":1}]',
 NULL),

-- Finance department → CFO (через map-селектор)
('augment-finance-cfo', 'augment', 130,
 '{"resource_type":"*","department":"finance"}',
 '[{"name":"CFO Approval","approvers":{"static":["cfo-001"],"dynamic":[]},"mode":"ALL","order":3}]',
 NULL),

-- Risk team → Risk approval (через map-селектор)
('augment-risk-group', 'augment', 125,
 '{"resource_type":"*","groups":["risk-team"]}',
 '[{"name":"Risk Review","approvers":{"static":["risk-officer"],"dynamic":[]},"mode":"ALL","order":3}]',
 NULL),

-- AUGMENT С DSL CONDITION

-- PCI-labeled ресурсы → compliance review
('augment-pci-compliance', 'augment', 140,
 '{"resource_type":"*","environment":"PROD"}',
 '[{"name":"Compliance Review","approvers":{"static":["compliance-officer"],"dynamic":[]},"mode":"ALL","order":4}]',
 '"pci" IN resource.labels'),

-- Finance + risk-team одновременно → усиленный контроль
('augment-finance-risk', 'augment', 150,
 '{"resource_type":"*"}',
 '[{"name":"Dual Control Review","approvers":{"static":["cro-001"],"dynamic":[{"role":"department_head"}]},"mode":"ALL","order":5}]',
 'hr.department == "finance" AND "risk-team" IN hr.groups'),

-- Делегированный запрос (не за себя) → HR BP должен подтвердить
('augment-delegation', 'augment', 110,
 '{"resource_type":"*"}',
 '[{"name":"HR BP Confirmation","approvers":{"dynamic":[{"role":"hr_bp"}],"static":[]},"mode":"ALL","order":2}]',
 'request.requested_for_user_id != "" AND subject.user_id != request.requested_for_user_id'),

-- Database в PROD + admin role → DBA lead (не просто team)
('augment-db-admin', 'augment', 135,
 '{"resource_type":"database","environment":"PROD","roles":["admin"]}',
 '[{"name":"DBA Lead Approval","approvers":{"static":["dba-lead"],"dynamic":[]},"mode":"ALL","order":3}]',
 NULL),

-- Critical label на любом ресурсе → VP approval
('augment-critical-vp', 'augment', 160,
 '{"resource_type":"*","environment":"PROD"}',
 '[{"name":"VP Approval","approvers":{"static":["vp-engineering"],"dynamic":[]},"mode":"ALL","order":5}]',
 '"critical" IN resource.labels'),


-- RESTRICT POLICIES (убирают шаги)


-- Для DEV-окружения убираем Security Review (если вдруг попал)
('restrict-dev-no-security', 'restrict', 10,
 '{"resource_type":"*","environment":"DEV"}',
 '[{"name":"Security Review","approvers":{"static":[],"dynamic":[]},"mode":"ANY","order":1}]',
 NULL),


-- OVERRIDE POLICIES (полная замена маршрута)

-- Экстренный доступ (break-glass) → только CISO
('override-break-glass', 'override', 200,
 '{"resource_type":"*","labels":["break-glass"]}',
 '[{"name":"CISO Emergency Approval","approvers":{"static":["ciso"],"dynamic":[]},"mode":"ALL","order":1}]',
 NULL),

-- Аудиторы — единственный approver: Head of Audit
('override-auditor', 'override', 190,
 '{"resource_type":"*","roles":["auditor"]}',
 '[{"name":"Audit Lead Approval","approvers":{"static":["head-of-audit"],"dynamic":[]},"mode":"ANY","order":1}]',
 NULL)

ON CONFLICT (policy_id) DO UPDATE SET
    type      = EXCLUDED.type,
    priority  = EXCLUDED.priority,
    selectors = EXCLUDED.selectors,
    steps     = EXCLUDED.steps,
    condition = EXCLUDED.condition,
    enabled   = TRUE;
