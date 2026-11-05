#!/usr/bin/env python3
"""Test-only 4-worker sweep overlay; old auto-reverse mode stays sequential."""
import json, pathlib, sys
root=pathlib.Path(__file__).resolve().parents[2]
out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
source_path=root/'internal/billing/reconciler/reconciler.go'
s=source_path.read_text()
anchor='func (r *Reconciler) SweepOnce(ctx context.Context) (Stats, error) {'
if s.count(anchor)!=1: raise ValueError('sweep anchor changed')
s=s.replace(anchor,'func (r *Reconciler) sweepSequential(ctx context.Context) (Stats, error) {',1)
new=r'''
// SweepOnce is a TEST-ONLY bounded per-page implementation.
func (r *Reconciler) SweepOnce(ctx context.Context) (Stats, error) {
 if !r.noAutoReverse { return r.sweepSequential(ctx) }
 var stats Stats
 cutoff:=time.Now().Add(-r.grace)
 base:=func()*gorm.DB {return r.db.WithContext(ctx).Model(&store.Bill{}).Where("phase = ? AND status = ? AND created_at < ?",store.PhaseReserve,store.StatusPending,cutoff)}
 var upperID uint64
 if err:=base().Select("COALESCE(MAX(id), 0)").Scan(&upperID).Error;err!=nil{return stats,err}
 var afterID uint64
 for afterID<upperID {
  var rows []store.Bill
  if err:=base().Where("id > ? AND id <= ?",afterID,upperID).Select("id", "request_id", "usage_reported_at", "created_at").Order("id ASC").Limit(sweepPageSize).Find(&rows).Error;err!=nil{return stats,err}
  if len(rows)==0 {break}
  afterID=rows[len(rows)-1].ID;stats.Scanned+=len(rows)
  var known []store.Bill
  for _,row:=range rows {
   if row.UsageReportedAt==nil {stats.Held++;if stats.HeldOldestCreatedAt.IsZero() || row.CreatedAt.Before(stats.HeldOldestCreatedAt){stats.HeldOldestCreatedAt=row.CreatedAt};continue}
   known=append(known,row)
  }
  type outcome struct { attempted bool; response *billingv1.SettleResponse; err error }
  results:=make([]outcome,len(known));jobs:=make(chan int,len(known))
  for i:=range known {jobs<-i};close(jobs)
  var wg sync.WaitGroup
  for i:=0;i<4;i++ {wg.Add(1);go func(){defer wg.Done();for index:=range jobs {
   if ctx.Err()!=nil {return}
   row:=known[index];child,cancel:=context.WithTimeout(ctx,settleRPCTimeout)
   response,err:=r.cli.Settle(child,&billingv1.SettleRequest{RequestId:row.RequestID,BillId:int64(row.ID)})
   cancel();results[index]=outcome{attempted:true,response:response,err:err}
  }}()}
  wg.Wait()
  for i,result:=range results {
   if !result.attempted {continue}
   row:=known[i]
   if result.err!=nil {stats.SettleErrors++;r.log.Error("recon settle","request_id",row.RequestID,"err",result.err);continue}
   if result.response==nil {stats.SettleErrors++;r.log.Error("recon settle","request_id",row.RequestID,"err","empty response");continue}
   if result.response.Settled {stats.Settled++;r.log.Info("recon settled","request_id",row.RequestID,"delta_quota",result.response.DeltaQuota,"bill_id",row.ID,"created_at",row.CreatedAt.Format(time.RFC3339))} else {r.log.Warn("recon settle skipped","request_id",row.RequestID,"reason","already finalized")}
  }
  if err:=ctx.Err();err!=nil{return stats,err}
 }
 return stats,nil
}
'''
pos=s.index('func (r *Reconciler) sweepSequential(')
s=s[:pos]+new+'\n'+s[pos:]
candidate=out/'reconciler.go';candidate.write_text(s)
(out/'overlay.json').write_text(json.dumps({'Replace':{str(source_path):str(candidate)}}))
