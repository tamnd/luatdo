// Question 10. Which duties have no identified bearer, and are they drafting
// defects or extraction failures?
//
// The graph cannot answer the second half and does not pretend to. It returns the
// quote that licensed the statement, which is the evidence a person needs to
// decide: if the quote names an actor the extraction lost it, and if the quote is
// a passive construction with no actor in it the drafting did.
MATCH (p:Component)-[:HAS_NORM]->(n:Norm)
WHERE n.norm_type = $norm_type AND coalesce(n.bearer, '') = ''
RETURN p.id AS provision,
       n.id AS norm,
       n.action AS action,
       n.object AS object,
       n.evidence_quote AS quote
ORDER BY provision
LIMIT $limit
