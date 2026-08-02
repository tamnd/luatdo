// Question 14. For one duty, what conditions must hold and what exceptions
// release the bearer from it?
//
// Conditions and exceptions are collected separately because they are not the
// same thing said two ways. A condition that fails means the duty never arose; an
// exception that applies means it arose and does not bind this person. Telling a
// client the wrong one of those is a different piece of advice.
MATCH (n:Norm {id: $norm})
OPTIONAL MATCH (n)-[:HAS_CONDITION]->(c:Condition)
OPTIONAL MATCH (n)-[:HAS_EXCEPTION]->(x:Exception)
OPTIONAL MATCH (n)-[:HAS_LEGAL_BASIS]->(p:Component)
RETURN n.norm_type AS norm_type,
       n.bearer AS bearer,
       n.action AS action,
       n.object AS object,
       p.id AS provision,
       [y IN collect(DISTINCT {kind: c.kind, text: c.text}) WHERE y.text IS NOT NULL] AS conditions,
       [y IN collect(DISTINCT {kind: x.kind, text: x.text}) WHERE y.text IS NOT NULL] AS exceptions
LIMIT $limit
