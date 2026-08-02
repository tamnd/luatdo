// Question 23. Which concept pairs does the corpus treat as alternatives?
//
// Support is the number of provisions the relation layer saw the pattern in and
// support_docs the number of instruments, and the second is the one to read. A
// pair with support 12 across one document is a drafting habit of one instrument;
// a pair with support 4 across four documents is a convention of the corpus.
MATCH (a:Concept)-[r:ALTERNATIVE_TO]->(b:Concept)
WHERE r.support >= $support
RETURN a.label_vi AS concept_a,
       b.label_vi AS concept_b,
       r.support AS provisions,
       r.support_docs AS instruments,
       r.confidence AS confidence,
       r.quote AS quote
ORDER BY instruments DESC, provisions DESC, concept_a
LIMIT $limit
