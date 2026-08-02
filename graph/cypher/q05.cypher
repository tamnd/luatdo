// Question 5. Every term whose definition differs between two instruments that
// are both in force.
//
// In force is asked of the temporal layer on a date rather than of a status
// property, so the same query answers the question as of any date. A pair where
// one side was repealed last year is a historical curiosity; a pair where both
// are in force today is a live contradiction somebody has to work around.
MATCH (a:TermUse)-[d:DIFFERS_FROM]->(b:TermUse)
WHERE a.doc_id <> b.doc_id
  AND EXISTS {
    MATCH (:Document {id: a.doc_id})-[:CONTAINS*0..8]->(:Component)-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)
    WHERE v.force = 'in_force' AND v.from_date <= $date AND (v.to_date = '' OR v.to_date > $date)
  }
  AND EXISTS {
    MATCH (:Document {id: b.doc_id})-[:CONTAINS*0..8]->(:Component)-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)
    WHERE v.force = 'in_force' AND v.from_date <= $date AND (v.to_date = '' OR v.to_date > $date)
  }
RETURN a.label_vi AS term,
       a.doc_id AS instrument_a, a.definition_vi AS definition_a,
       b.doc_id AS instrument_b, b.definition_vi AS definition_b,
       d.rationale AS recorded_because
ORDER BY term
LIMIT $limit
