// Question 8. Which defined terms were introduced by an amendment rather than by
// the original instrument?
//
// A definition added years after enactment is one the drafters found they needed,
// and the instrument that added it is usually the one that says why. The event
// kind separates the two ways that happens: an amendment rewrote a provision that
// now defines the term, or a supplement inserted the definition outright.
MATCH (p:Component)-[:DEFINES_TERM]->(t:TermUse)
MATCH (p)-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)<-[:PRODUCES_VERSION]-(e:Event)
WHERE e.kind IN ['amend', 'supplement']
OPTIONAL MATCH (e)-[:CAUSED_BY]->(amending:Document)
RETURN t.label_vi AS term,
       t.doc_id AS instrument,
       p.id AS provision,
       e.kind AS introduced_by,
       e.date AS introduced_on,
       coalesce(amending.official_number, e.caused_by_doc) AS amending_instrument
ORDER BY introduced_on, term
LIMIT $limit
