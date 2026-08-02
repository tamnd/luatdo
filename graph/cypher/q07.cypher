// Question 7. The hierarchy under one concept as the corpus actually uses it,
// with the inconsistencies flagged.
//
// CONCEPT_BROADER is the relation layer's edge, renamed away from BROADER, which
// the subject taxonomy owns. The flag is not a defect report: a concept with two
// broader parents is sometimes a genuine multiple inheritance and sometimes two
// instruments disagreeing about where a thing sits, and the graph cannot tell
// those apart. It can point at them, which is what the question asked for.
MATCH path = (leaf:Concept)-[:CONCEPT_BROADER*1..6]->(root:Concept {id: $concept})
OPTIONAL MATCH (leaf)-[:CONCEPT_BROADER]->(parent:Concept)
WITH leaf, path, count(DISTINCT parent) AS parents
RETURN [n IN nodes(path) | coalesce(n.label_vi, n.id)] AS chain,
       length(path) AS depth,
       leaf.id AS concept,
       parents AS broader_parents,
       parents > 1 AS inconsistent
ORDER BY depth, concept
LIMIT $limit
