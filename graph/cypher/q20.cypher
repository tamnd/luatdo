// Question 20. A concept was redefined by a later instrument. Which earlier norms
// mentioning it are now potentially affected, and which of them were never
// amended?
//
// Potentially is doing real work in that sentence and the query keeps it. A norm
// written against the old definition may be unaffected, may have been quietly
// broadened, or may now say something nobody intended, and no graph can tell which
// without a person reading it. What the graph can do is produce the list, and
// separate the provisions somebody has revisited since from the ones nobody has
// touched. The second group is where the surprises are.
MATCH (later:TermUse)-[:DIFFERS_FROM]-(earlier:TermUse)
MATCH (later)-[:INSTANCE_OF]->(c:Concept)<-[:INSTANCE_OF]-(earlier)
MATCH (ld:Document {id: later.doc_id})
MATCH (ed:Document {id: earlier.doc_id})
WHERE ed.effective_from <> '' AND ld.effective_from > ed.effective_from
MATCH (d:Document)-[:CONTAINS*1..8]->(p:Component)-[:HAS_NORM]->(n:Norm)-[:ABOUT_CONCEPT]->(c)
WHERE d.effective_from <> '' AND d.effective_from < ld.effective_from
RETURN DISTINCT c.label_vi AS concept,
       ld.id AS redefined_by,
       ld.effective_from AS redefined_on,
       d.id AS affected_document,
       p.id AS affected_provision,
       n.action AS action,
       NOT EXISTS {
         MATCH (p)-[:HAS_TEMPORAL_VERSION]->(:TemporalVersion)<-[:PRODUCES_VERSION]-(e:Event)
         WHERE e.kind IN ['amend', 'supplement'] AND e.date > ld.effective_from
       } AS never_revisited
ORDER BY never_revisited DESC, concept, affected_provision
LIMIT $limit
