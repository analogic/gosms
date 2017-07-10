$(function() {

  var logTable = $('#incoming').dataTable({
    "data": [],
    "iDisplayLength": 5,
    "bLengthChange": false,
    "oLanguage": { "sSearch": "" },
    "order": [[ 0, "desc" ]],
    "columns": [
        { "data": "id" },
        { "data": "created_at" },
        { "data": "mobile" },
        { "data": "body" }
    ]
  });
  
  var loadData = function() {
    $.ajax({
      url: "/api/incoming/"
    })
    .done(function(logs) {
      if(!logs.messages) {
        return
      }
      logTable.fnClearTable(logs.messages);
      logTable.fnAddData(logs.messages);
    })
  };

  loadData();
});