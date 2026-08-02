// Question 18. The amendment history of one provision, as a chain of events with
// the instrument that caused each.
//
// Ordered by sequence rather than by date, because two amendments can take effect
// on the same day and the sequence is what the layer decided the order was. The
// instruction is the amending instrument's own words, so a reader who thinks the
// chain is wrong can see what it was built from.
MATCH (c:Component {id: $component})-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)
OPTIONAL MATCH (opened:Event)-[:PRODUCES_VERSION]->(v)
OPTIONAL MATCH (closed:Event)-[:TERMINATES]->(v)
OPTIONAL MATCH (opened)-[:CAUSED_BY]->(by:Document)
RETURN v.seq AS seq,
       v.from_date AS from_date,
       v.to_date AS to_date,
       v.force AS force,
       opened.kind AS opened_by,
       coalesce(by.official_number, opened.caused_by_doc) AS instrument,
       opened.instruction AS instruction,
       closed.kind AS closed_by
ORDER BY seq
LIMIT $limit
