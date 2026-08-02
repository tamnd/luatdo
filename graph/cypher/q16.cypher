// Question 16. What did one provision require on a given date?
//
// This is the question the temporal layer exists for, and the one most likely to
// be answered wrongly by a graph that has only TextVersion nodes. A TextVersion is
// the text as one instrument printed it. A TemporalVersion is what the provision
// said over an interval, which is what somebody asking about a date means. Run it
// twice with two dates to compare.
MATCH (c:Component {id: $component})-[:HAS_TEMPORAL_VERSION]->(v:TemporalVersion)
WHERE v.from_date <= $date AND (v.to_date = '' OR v.to_date > $date)
OPTIONAL MATCH (c)-[:HAS_NORM]->(n:Norm)
OPTIONAL MATCH (v)-[:INCLUDES*1..4]->(part:TemporalVersion)
RETURN v.id AS version,
       v.seq AS seq,
       v.force AS force,
       v.from_date AS from_date,
       v.to_date AS to_date,
       v.text AS text,
       [x IN collect(DISTINCT part.text) WHERE x IS NOT NULL AND x <> ''] AS included_text,
       [x IN collect(DISTINCT n.action) WHERE x IS NOT NULL] AS actions
LIMIT $limit
