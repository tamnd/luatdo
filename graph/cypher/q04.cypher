// Question 4. Where is a term defined, and do two instruments define it the same
// way?
//
// The answer is one row per instrument that defines it, which is the point: a
// query that returned one row has either found a term only one instrument
// defines or has merged two definitions that differ. A DIFFERS_FROM edge means
// somebody read both and recorded that they do not agree, and its absence means
// nobody has looked yet rather than that they agree.
MATCH (t:TermUse)
WHERE t.label_vi = $term OR $term IN t.aliases
OPTIONAL MATCH (t)-[:INSTANCE_OF]->(c:Concept)
OPTIONAL MATCH (t)-[d:DIFFERS_FROM]-(other:TermUse)
RETURN t.doc_id AS instrument,
       t.defined_by AS provision,
       t.label_vi AS term,
       t.definition_vi AS definition,
       c.id AS concept,
       [x IN collect(DISTINCT other.doc_id) WHERE x IS NOT NULL] AS differs_from,
       [x IN collect(DISTINCT d.rationale) WHERE x IS NOT NULL AND x <> ''] AS recorded_because
ORDER BY instrument
LIMIT $limit
