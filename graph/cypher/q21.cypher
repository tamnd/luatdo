// Question 21. What must exist before a permit can be issued, following only
// concept level prerequisite relations and not the text?
//
// Only REQUIRES is walked, so the answer is what the relation layer believes
// rather than what any one provision says. That is the point of asking it this
// way: if the chain is wrong the relation layer is wrong, and a query that fell
// back to the text would hide that.
MATCH path = (c:Concept {id: $concept})-[:REQUIRES*1..6]->(pre:Concept)
RETURN [n IN nodes(path) | coalesce(n.label_vi, n.id)] AS chain,
       length(path) AS depth,
       pre.id AS prerequisite,
       pre.label_vi AS prerequisite_label
ORDER BY depth, prerequisite
LIMIT $limit
