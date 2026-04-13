INSERT INTO policies (policy_id, type, priority, selectors, steps, conditional_steps) VALUES

-- BASELINE: по resource_type + environment


('base-app-prod', 'baseline', 100,
 '{"resource_type":"app","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":["team-lead-pool"],"dynamic":[{"role":"manager"}]},"mode":"ANY","order":1}]',
 '[
    {"if":"\"pci\" IN resource.labels","steps":[{"name":"PCI Compliance","approvers":{"static":["compliance-officer","pci-auditor"],"dynamic":[]},"mode":"ALL","order":4}]},
    {"if":"\"critical\" IN resource.labels","steps":[{"name":"VP Approval","approvers":{"static":["vp-engineering","vp-security"],"dynamic":[]},"mode":"ALL","order":5}]},
    {"if":"\"hipaa\" IN resource.labels","steps":[{"name":"HIPAA Review","approvers":{"static":["hipaa-officer","privacy-counsel"],"dynamic":[]},"mode":"ALL","order":4}]}
  ]'),

('base-db-prod', 'baseline', 100,
 '{"resource_type":"database","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"DBA Review","approvers":{"static":["dba-senior-1","dba-senior-2"],"dynamic":[]},"mode":"ANY","order":2}]',
 '[
    {"if":"\"pci\" IN resource.labels","steps":[{"name":"PCI Compliance","approvers":{"static":["compliance-officer","pci-auditor"],"dynamic":[]},"mode":"ALL","order":4}]},
    {"if":"resource.attributes.size == \"large\"","steps":[{"name":"Capacity Review","approvers":{"static":["infra-capacity","dba-lead"],"dynamic":[]},"mode":"ALL","order":3}]}
  ]'),

('base-server-prod', 'baseline', 100,
 '{"resource_type":"server","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"Infra Review","approvers":{"static":["infra-lead","infra-oncall"],"dynamic":[]},"mode":"ANY","order":2}]',
 '[]'),

('base-network-prod', 'baseline', 100,
 '{"resource_type":"network","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"Network Security","approvers":{"static":["netsec-lead","netsec-engineer"],"dynamic":[]},"mode":"ALL","order":2}]',
 '[]'),

('base-storage-prod', 'baseline', 100,
 '{"resource_type":"storage","environment":"PROD"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1},{"name":"Storage Review","approvers":{"static":["storage-admin","storage-oncall"],"dynamic":[]},"mode":"ANY","order":2}]',
 '[]'),

('base-any-stage', 'baseline', 60,
 '{"resource_type":"*","environment":"STAGE"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1}]',
 '[]'),

('base-any-dev', 'baseline', 50,
 '{"resource_type":"*","environment":"DEV"}',
 '[{"name":"Manager Approval","approvers":{"dynamic":[{"role":"manager"}],"static":[]},"mode":"ANY","order":1}]',
 '[]'),

-- AUGMENT: узкие селекторы, несколько approvers в шагах

-- Security review ТОЛЬКО для app в PROD (не для database/server)
('augment-security-app-prod', 'augment', 90,
 '{"resource_type":"app","environment":"PROD"}',
 '[{"name":"Security Review","approvers":{"static":["appsec-lead","appsec-engineer"],"dynamic":[]},"mode":"ALL","order":2}]',
 '[]'),

-- Admin на app в PROD — двое approvers
('augment-app-admin-prod', 'augment', 120,
 '{"resource_type":"app","environment":"PROD","roles":["admin"]}',
 '[{"name":"App Admin Approval","approvers":{"static":["app-admin-approver","security-admin"],"dynamic":[]},"mode":"ALL","order":1}]',
 '[]'),

-- Admin на database в PROD
('augment-db-admin-prod', 'augment', 120,
 '{"resource_type":"database","environment":"PROD","roles":["admin"]}',
 '[{"name":"DBA Admin Approval","approvers":{"static":["dba-lead","dba-security"],"dynamic":[]},"mode":"ALL","order":3}]',
 '[]'),

-- Admin на server в PROD
('augment-server-admin-prod', 'augment', 120,
 '{"resource_type":"server","environment":"PROD","roles":["admin"]}',
 '[{"name":"Infra Admin Approval","approvers":{"static":["infra-director","infra-security"],"dynamic":[]},"mode":"ALL","order":3}]',
 '[]'),

-- DBA role на database (не admin, а dba)
('augment-db-dba-role', 'augment', 115,
 '{"resource_type":"database","roles":["dba"]}',
 '[{"name":"Senior DBA Sign-off","approvers":{"static":["dba-lead"],"dynamic":[{"role":"manager"}]},"mode":"ANY","order":2}]',
 '[]'),

-- Finance department — CFO + условно dual control при risk
('augment-finance', 'augment', 130,
 '{"resource_type":"*","environment":"PROD","department":"finance"}',
 '[{"name":"CFO Approval","approvers":{"static":["cfo","deputy-cfo"],"dynamic":[]},"mode":"ANY","order":3}]',
 '[
    {"if":"\"risk-team\" IN hr.groups","steps":[{"name":"Dual Control","approvers":{"static":["cro"],"dynamic":[{"role":"department_head"}]},"mode":"ALL","order":5}]}
  ]'),

-- Risk team — risk review
('augment-risk-group', 'augment', 125,
 '{"resource_type":"*","environment":"PROD","groups":["risk-team"]}',
 '[{"name":"Risk Review","approvers":{"static":["risk-officer","risk-analyst"],"dynamic":[]},"mode":"ALL","order":3}]',
 '[]'),

-- Legal team
('augment-legal-group', 'augment', 125,
 '{"resource_type":"*","environment":"PROD","groups":["legal-team"]}',
 '[{"name":"Legal Review","approvers":{"static":["legal-counsel","legal-director"],"dynamic":[]},"mode":"ALL","order":3}]',
 '[]'),

-- Делегированный запрос — шаг только при делегации
('augment-delegation', 'augment', 110,
 '{"resource_type":"*","environment":"PROD"}',
 '[]',
 '[
    {"if":"request.requested_for_user_id != \"\" AND subject.user_id != request.requested_for_user_id","steps":[{"name":"HR BP Delegation Check","approvers":{"dynamic":[{"role":"hr_bp"},{"role":"manager"}],"static":[]},"mode":"ALL","order":2}]}
  ]'),

-- Contractor (не active) на PROD
('augment-contractor-prod', 'augment', 105,
 '{"resource_type":"*","environment":"PROD"}',
 '[]',
 '[
    {"if":"NOT hr.status == \"active\"","steps":[{"name":"Contractor Review","approvers":{"dynamic":[{"role":"department_head"}],"static":["contractor-mgmt"],"dynamic":[{"role":"department_head"}]},"mode":"ALL","order":2}]}
  ]'),

-- Конкретный ресурс по имени — billing app в PROD
('augment-billing-app', 'augment', 140,
 '{"resource_type":"app","resource_name":"billing","environment":"PROD"}',
 '[{"name":"Billing Owner Approval","approvers":{"static":["billing-owner","billing-oncall"],"dynamic":[]},"mode":"ANY","order":2}]',
 '[]'),


-- RESTRICT

('restrict-dev-no-security', 'restrict', 10,
 '{"resource_type":"*","environment":"DEV"}',
 '[{"name":"Security Review","approvers":{"static":[],"dynamic":[]},"mode":"ANY","order":1}]',
 '[]'),

('restrict-stage-no-compliance', 'restrict', 10,
 '{"resource_type":"*","environment":"STAGE"}',
 '[{"name":"PCI Compliance","approvers":{"static":[],"dynamic":[]},"mode":"ANY","order":1}]',
 '[]'),


-- OVERRIDE


('override-break-glass', 'override', 200,
 '{"resource_type":"*","labels":["break-glass"]}',
 '[{"name":"CISO Emergency","approvers":{"static":["ciso","deputy-ciso"],"dynamic":[]},"mode":"ALL","order":1}]',
 '[]'),

('override-auditor', 'override', 190,
 '{"resource_type":"*","roles":["auditor"]}',
 '[{"name":"Audit Lead","approvers":{"static":["head-of-audit","senior-auditor"],"dynamic":[]},"mode":"ANY","order":1}]',
 '[]')

ON CONFLICT (policy_id) DO UPDATE SET
    type              = EXCLUDED.type,
    priority          = EXCLUDED.priority,
    selectors         = EXCLUDED.selectors,
    steps             = EXCLUDED.steps,
    conditional_steps = EXCLUDED.conditional_steps,
    enabled           = TRUE;
