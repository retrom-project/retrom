-- Reference counts are maintained by explicit application writes in the owning transaction.
CREATE INDEX blobs_zero_references ON blobs(id) WHERE ref_count=0;
