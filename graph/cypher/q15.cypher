// Question 15. Which norms name an authority that no provision in the corpus ever
// resolves?
//
// "Cơ quan có thẩm quyền" is a role, not an institution. It resolves per document
// and often per procedure, and where nothing resolves it the reader is left to
// guess which ministry to file with. The test is that no term use of the concept
// the norm points at carries a definition, which is the graph's way of saying
// nobody wrote down who this is.
MATCH (p:Component)-[:HAS_NORM]->(n:Norm)
WHERE (n.bearer CONTAINS $role OR n.counterparty CONTAINS $role)
  AND NOT EXISTS {
    MATCH (n)-[:ABOUT_CONCEPT]->(:Concept)<-[:INSTANCE_OF]-(t:TermUse)
    WHERE t.definition_vi <> ''
  }
RETURN p.id AS provision,
       n.bearer AS bearer,
       n.counterparty AS counterparty,
       n.action AS action,
       n.evidence_quote AS quote
ORDER BY provision
LIMIT $limit
