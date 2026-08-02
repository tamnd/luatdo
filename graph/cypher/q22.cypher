// Question 22. Which concepts are required by a norm in one instrument and
// defined in another, and are those two instruments connected in the citation
// graph at all?
//
// A concept defined somewhere the using instrument never cites is a silent
// dependency: a reader of the using instrument has no way to discover the
// definition from the text in front of them. Those rows sort first, because they
// are the ones worth doing something about.
MATCH (n:Norm)-[:ABOUT_CONCEPT]->(c:Concept)<-[:INSTANCE_OF]-(t:TermUse)
WHERE t.definition_vi <> ''
MATCH (using:Document)-[:CONTAINS*1..8]->(p:Component)-[:HAS_NORM]->(n)
WHERE using.id <> t.doc_id
OPTIONAL MATCH citation = shortestPath((using)-[:CITES|AMENDS*1..6]-(:Document {id: t.doc_id}))
RETURN DISTINCT c.label_vi AS concept,
       using.id AS uses_it,
       p.id AS provision,
       t.doc_id AS defines_it,
       citation IS NOT NULL AS connected_by_citation,
       CASE WHEN citation IS NULL THEN -1 ELSE length(citation) END AS citation_hops
ORDER BY connected_by_citation, concept, provision
LIMIT $limit
