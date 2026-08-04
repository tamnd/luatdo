// Question 24. What follows from an act, as a chain the corpus states rather
// than a chain one provision asserts?
//
// Every step carries its status, its support and the verdict of the blind
// direction pass, and none of the three is a filter here. A consequence graph
// answers with what it has, and hiding the weak steps would turn a chain that
// rests on one sentence into a chain that reads like settled law. A step whose
// direction came back flipped is in the graph because somebody has to look at
// it, and it is the row to distrust first.
MATCH path = (a:Act {id: $act})-[chain:TRIGGERS|PRECEDES|PRECONDITION_OF|PRECLUDES*1..4]->(next:Act)
RETURN [n IN nodes(path) | n.label_vi] AS chain,
       [r IN relationships(path) | type(r)] AS steps,
       length(path) AS depth,
       next.label_vi AS consequence,
       next.status AS act_status,
       [r IN relationships(path) | r.status] AS step_status,
       [r IN relationships(path) | r.support] AS provisions,
       [r IN relationships(path) | r.support_docs] AS instruments,
       [r IN relationships(path) | r.direction] AS direction,
       [r IN relationships(path) | r.provision_id] AS stated_in
ORDER BY depth, consequence
LIMIT $limit
