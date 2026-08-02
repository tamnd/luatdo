// Question 2. Which currently in force documents cite a document that has been
// repealed?
//
// Repeal is read off the temporal layer rather than off a status property,
// because a status is a summary somebody wrote and the event chain is what the
// amending instruments actually said. Expiry and replacement count: a citation
// into a document that expired is as broken as one into a document that was
// repealed by name.
MATCH (citing:Document)-[:CONTAINS*0..8]->(source)-[:CITES]->(target:Document)
MATCH (e:Event)-[:TERMINATES]->(v:TemporalVersion)
WHERE v.doc_id = target.id
  AND e.kind IN ['repeal', 'replace', 'expire']
  AND citing.status <> 'repealed'
  AND citing <> target
RETURN DISTINCT citing.id AS in_force_document,
       target.id AS repealed_document,
       e.kind AS how,
       e.date AS repealed_on
ORDER BY repealed_on DESC, in_force_document
LIMIT $limit
