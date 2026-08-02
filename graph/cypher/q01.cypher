// Question 1. Which documents does one document amend, transitively?
//
// The path walks down through CONTAINS to reach the provision that carries the
// amending citation, then out along AMENDS, and repeats. CONTAINS never ends at
// a Document, so the last hop of any path that lands on one is necessarily an
// AMENDS edge and no extra filter is needed to say so.
MATCH path = (start:Document {id: $doc})-[:CONTAINS|AMENDS*1..8]->(target:Document)
WHERE target <> start
RETURN target.id AS amended,
       target.official_number AS official_number,
       target.title AS title,
       min(length(path)) AS hops
ORDER BY hops, amended
LIMIT $limit
