// Question 26. Which acts does more than one instrument name, and who takes
// part in them?
//
// This is the question the layer's identity decision is judged by, and it is
// also the question that shows the decision going wrong. An act is one node
// corpus wide, so a law and the decree guiding it land on the same filing, which
// is the only reason a chain ever crosses a document boundary. The same rule
// merges two unrelated acts the moment two drafters reach for one phrase, and
// the docs list is there so a reader can see which instruments were merged
// before they believe the row.
MATCH (a:Act)
WHERE a.support_docs >= $min_docs
OPTIONAL MATCH (a)-[part:HAS_PARTICIPANT]->(c:Concept)
RETURN a.label_vi AS act,
       a.class AS class,
       a.status AS status,
       a.support AS provisions,
       a.support_docs AS instruments,
       a.docs AS named_in,
       a.aliases AS also_written,
       collect(part.role + ": " + c.label_vi) AS participants
ORDER BY instruments DESC, provisions DESC, act
LIMIT $limit
