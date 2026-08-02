// Question 11. What must a business do to obtain a permit, as a sequence of steps
// with their document requirements and deadlines?
//
// The procedure identifier is what makes this a sequence rather than a pile of
// norms that happen to mention the same permit. A step with no number sorts last,
// which is a visible gap rather than a silently reordered procedure.
MATCH (p:Component)-[:HAS_NORM]->(n:Norm)
WHERE n.procedure_id = $procedure
OPTIONAL MATCH (n)-[:HAS_CONDITION]->(c:Condition)
RETURN n.step AS step,
       n.bearer AS actor,
       n.action AS action,
       n.object AS object,
       n.deadline_text AS deadline,
       p.id AS provision,
       [x IN collect(DISTINCT c.text) WHERE x IS NOT NULL] AS requirements
ORDER BY step, provision
LIMIT $limit
